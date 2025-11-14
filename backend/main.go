package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/imalyk/go-video-processor/pkg/job"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
)

const (
	defaultMaxUploadSize = 2 << 30 // 2 GiB
)

func main() {
	ctx := context.Background()
	cfg := loadConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	server, err := newServer(ctx, cfg, logger)
	if err != nil {
		log.Fatalf("failed to initialise server: %v", err)
	}

	addr := fmt.Sprintf(":%s", cfg.HTTPPort)
	logger.Info("starting backend server", "addr", addr)

	if err := http.ListenAndServe(addr, server.router); err != nil {
		logger.Error("server stopped with error", "error", err)
	}
}

type config struct {
	HTTPPort          string
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	RedisQueueKey     string
	JobIndexKey       string
	UploadsBucket     string
	ProcessedBucket   string
	MinioEndpoint     string
	MinioAccessKey    string
	MinioSecretKey    string
	MinioUseSSL       bool
	MinioRegion       string
	FrontendDir       string
	PresignExpiry     time.Duration
	LogLevel          slog.Level
	MaxUploadSize     int64
}

func loadConfig() config {
	redisDB := mustParseInt(os.Getenv("REDIS_DB"), 0)

	presignTTL := mustParseDuration(os.Getenv("DOWNLOAD_URL_TTL"), 15*time.Minute)
	maxUpload := mustParseInt64(os.Getenv("MAX_UPLOAD_BYTES"), defaultMaxUploadSize)

	logLevel := slog.LevelInfo
	if lvlStr := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))); lvlStr != "" {
		switch lvlStr {
		case "debug":
			logLevel = slog.LevelDebug
		case "warn", "warning":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	return config{
		HTTPPort:        defaultString(os.Getenv("PORT"), "8080"),
		RedisAddr:       defaultString(os.Getenv("REDIS_ADDR"), "localhost:6379"),
		RedisPassword:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		RedisQueueKey:   defaultString(os.Getenv("REDIS_QUEUE_KEY"), "video:jobs:queue"),
		JobIndexKey:     defaultString(os.Getenv("REDIS_JOB_INDEX_KEY"), "video:jobs:index"),
		UploadsBucket:   defaultString(os.Getenv("MINIO_UPLOADS_BUCKET"), "video-uploads"),
		ProcessedBucket: defaultString(os.Getenv("MINIO_PROCESSED_BUCKET"), "video-processed"),
		MinioEndpoint:   defaultString(os.Getenv("MINIO_ENDPOINT"), "localhost:9000"),
		MinioAccessKey:  defaultString(os.Getenv("MINIO_ACCESS_KEY"), "minio"),
		MinioSecretKey:  defaultString(os.Getenv("MINIO_SECRET_KEY"), "minio123"),
		MinioUseSSL:     strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true"),
		MinioRegion:     os.Getenv("MINIO_REGION"),
		FrontendDir:     defaultString(os.Getenv("FRONTEND_DIR"), "../frontend"),
		PresignExpiry:   presignTTL,
		LogLevel:        logLevel,
		MaxUploadSize:   maxUpload,
	}
}

type server struct {
	router          *mux.Router
	logger          *slog.Logger
	cfg             config
	redis           *redis.Client
	minio           *minio.Client
	ctx             context.Context
	queueKey        string
	jobIndexKey     string
	uploadsBucket   string
	processedBucket string
	presignTTL      time.Duration
}

func newServer(ctx context.Context, cfg config, logger *slog.Logger) (*server, error) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
		Region: cfg.MinioRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("minio connection: %w", err)
	}

	if err := ensureBucket(ctx, minioClient, cfg.UploadsBucket, cfg.MinioRegion); err != nil {
		return nil, fmt.Errorf("ensure uploads bucket: %w", err)
	}

	if err := ensureBucket(ctx, minioClient, cfg.ProcessedBucket, cfg.MinioRegion); err != nil {
		return nil, fmt.Errorf("ensure processed bucket: %w", err)
	}

	s := &server{
		router:          mux.NewRouter(),
		logger:          logger,
		cfg:             cfg,
		redis:           redisClient,
		minio:           minioClient,
		ctx:             ctx,
		queueKey:        cfg.RedisQueueKey,
		jobIndexKey:     cfg.JobIndexKey,
		uploadsBucket:   cfg.UploadsBucket,
		processedBucket: cfg.ProcessedBucket,
		presignTTL:      cfg.PresignExpiry,
	}

	s.registerRoutes()
	return s, nil
}

func ensureBucket(ctx context.Context, client *minio.Client, bucket, region string) error {
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	opts := minio.MakeBucketOptions{
		Region:        region,
		ObjectLocking: false,
	}
	return client.MakeBucket(ctx, bucket, opts)
}

func (s *server) registerRoutes() {
	s.router.HandleFunc("/healthz", s.handleHealth).Methods(http.MethodGet)
	s.router.HandleFunc("/uploads", s.handleUpload).Methods(http.MethodPost)
	s.router.HandleFunc("/status/{jobID}", s.handleStatus).Methods(http.MethodGet)
	s.router.HandleFunc("/download/{jobID}", s.handleDownload).Methods(http.MethodGet)
	s.router.HandleFunc("/jobs", s.handleListJobs).Methods(http.MethodGet)

	frontend := http.StripPrefix("/", http.FileServer(http.Dir(s.cfg.FrontendDir)))
	s.router.PathPrefix("/").Handler(frontend).Methods(http.MethodGet)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadSize)
	if err := r.ParseMultipartForm(s.cfg.MaxUploadSize); err != nil {
		s.logger.Warn("failed to parse multipart form", "error", err)
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}
	defer func() {
		_ = r.MultipartForm.RemoveAll()
	}()

	file, header, err := r.FormFile("video")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing video file")
		return
	}
	defer file.Close()

	objectName := buildObjectName(header)
	jobID := uuid.NewString()
	originalObject := fmt.Sprintf("%s/original/%s", jobID, objectName)

	targetContainer := strings.ToLower(defaultString(r.FormValue("container"), "mp4"))
	targetCodec := defaultString(r.FormValue("codec"), "libx264")
	targetBase := strings.TrimSuffix(objectName, filepath.Ext(objectName))
	if targetBase == "" {
		targetBase = "video"
	}
	targetObject := fmt.Sprintf("%s/processed/%s_processed.%s", jobID, targetBase, targetContainer)

	size := header.Size
	if size <= 0 {
		size = -1
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uploadInfo, err := s.minio.PutObject(ctx, s.uploadsBucket, originalObject, file, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		s.logger.Error("failed to upload to minio", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store upload")
		return
	}

	now := time.Now().UTC()
	j := job.Job{
		ID:               jobID,
		Status:           job.StatusQueued,
		Progress:         0,
		Attempts:         0,
		OriginalFile:     originalObject,
		ProcessedFile:    "",
		OriginalFilename: header.Filename,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.storeJob(ctx, j); err != nil {
		s.logger.Error("failed to store job metadata", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record job")
		return
	}

	msg := job.Message{
		JobID:           jobID,
		InputBucket:     s.uploadsBucket,
		InputObject:     originalObject,
		OutputBucket:    s.processedBucket,
		OutputObject:    targetObject,
		OriginalName:    header.Filename,
		TargetCodec:     targetCodec,
		TargetContainer: targetContainer,
	}

	if err := s.enqueueJob(ctx, msg); err != nil {
		s.logger.Error("failed to enqueue job", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to enqueue job")
		return
	}

	s.logger.Info("job enqueued", "job_id", jobID, "size", uploadInfo.Size)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id": jobID,
		"status": string(j.Status),
	})
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["jobID"]
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "job id required")
		return
	}

	j, err := s.loadJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("failed to load job", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}

	writeJSON(w, http.StatusOK, j)
}

func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["jobID"]
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "job id required")
		return
	}

	j, err := s.loadJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("failed to load job", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}

	if j.Status != job.StatusCompleted {
		writeError(w, http.StatusConflict, "job not completed")
		return
	}

	if j.ProcessedFile == "" {
		writeError(w, http.StatusInternalServerError, "processed file unavailable")
		return
	}

	reqParams := url.Values{}
	reqParams.Set("response-content-disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadableFilename(j)))

	presigned, err := s.minio.PresignedGetObject(r.Context(), s.processedBucket, j.ProcessedFile, s.presignTTL, reqParams)
	if err != nil {
		s.logger.Error("failed to presign object", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate download url")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"job_id":        jobID,
		"download_url":  presigned.String(),
		"original_name": j.OriginalFilename,
	})
}

func (s *server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := mustParseInt(defaultString(r.URL.Query().Get("limit"), "50"), 50)
	if limit <= 0 {
		limit = 50
	}

	ids, err := s.redis.ZRevRange(ctx, s.jobIndexKey, 0, int64(limit-1)).Result()
	if err != nil {
		s.logger.Error("failed to fetch job ids", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	jobs := make([]job.Job, 0, len(ids))
	for _, id := range ids {
		j, err := s.loadJob(ctx, id)
		if err != nil {
			if errors.Is(err, errNotFound) {
				continue
			}
			s.logger.Warn("skipping job due to load error", "job_id", id, "error", err)
			continue
		}
		jobs = append(jobs, j)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"jobs": jobs,
	})
}

func (s *server) storeJob(ctx context.Context, j job.Job) error {
	fields := map[string]interface{}{
		"id":                j.ID,
		"status":            j.Status,
		"progress":          j.Progress,
		"attempts":          j.Attempts,
		"original_file":     j.OriginalFile,
		"processed_file":    j.ProcessedFile,
		"error":             j.ErrorMessage,
		"original_filename": j.OriginalFilename,
		"created_at":        j.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":        j.UpdatedAt.Format(time.RFC3339Nano),
	}

	if err := s.redis.HSet(ctx, jobKey(j.ID), fields).Err(); err != nil {
		return err
	}

	return s.redis.ZAdd(ctx, s.jobIndexKey, redis.Z{
		Score:  float64(j.CreatedAt.UnixMicro()),
		Member: j.ID,
	}).Err()
}

func (s *server) updateJob(ctx context.Context, j job.Job) error {
	j.UpdatedAt = time.Now().UTC()
	return s.storeJob(ctx, j)
}

var errNotFound = errors.New("job not found")

func (s *server) loadJob(ctx context.Context, jobID string) (job.Job, error) {
	fields, err := s.redis.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return job.Job{}, err
	}
	if len(fields) == 0 {
		return job.Job{}, errNotFound
 	}

	var j job.Job
	j.ID = jobID
	j.Status = job.Status(fields["status"])
	j.OriginalFile = fields["original_file"]
	j.ProcessedFile = fields["processed_file"]
	j.ErrorMessage = fields["error"]
	j.OriginalFilename = fields["original_filename"]
	j.Progress = mustParseInt64(fields["progress"], 0)
	j.Attempts = mustParseInt64(fields["attempts"], 0)
	j.CreatedAt = mustParseTime(fields["created_at"])
	j.UpdatedAt = mustParseTime(fields["updated_at"])

	return j, nil
}

func (s *server) enqueueJob(ctx context.Context, msg job.Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.redis.RPush(ctx, s.queueKey, payload).Err()
}

func buildObjectName(header *multipart.FileHeader) string {
	name := filepath.Base(header.Filename)
	name = strings.ReplaceAll(name, " ", "_")
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "video"
	}
	return name
}

func downloadableFilename(j job.Job) string {
	if j.OriginalFilename == "" {
		return fmt.Sprintf("%s.mp4", j.ID)
	}
	base := strings.TrimSuffix(filepath.Base(j.OriginalFilename), filepath.Ext(j.OriginalFilename))
	return fmt.Sprintf("%s_processed.mp4", base)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func mustParseInt(val string, fallback int) int {
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func mustParseInt64(val string, fallback int64) int64 {
	if val == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func mustParseDuration(val string, fallback time.Duration) time.Duration {
	if val == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func mustParseTime(val string) time.Time {
	if val == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, val); err == nil {
		return t
	}
	return time.Time{}
}

func jobKey(id string) string {
	return fmt.Sprintf("job:%s", id)
}








