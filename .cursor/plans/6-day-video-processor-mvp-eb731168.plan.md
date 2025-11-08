<!-- eb731168-1979-45f9-8673-be8fd9c9dc5f 844325fd-5224-453d-b924-91fc21d6ea20 -->
# 6-Day Video Processor MVP Plan

## Day 1: Fix Foundation & Learn Go Basics (10h)

**Goal:** Clean up syntax errors, understand Go fundamentals, get basic backend running

**Learning Focus:** Go syntax, structs, methods, error handling, HTTP handlers

### Tasks:

1. Fix syntax errors in `backend/main.go` (lines 19-27, 56, 94, 103, 136-193)

- Struct field tags formatting
- Function receiver syntax
- Variable declarations
- Method signatures

2. Implement proper project structure with Go modules

- Initialize `go.mod` in backend and worker
- Import necessary packages (gorilla/mux, redis client, minio client)

3. Create clean `Job` struct with proper tags
4. Implement working `uploadFileHandler` with proper error handling
5. Test basic upload flow without processing

**Deliverable:** Backend accepts file uploads and returns job ID

---

## Day 2: Job Queue & Redis Integration (10h)

**Goal:** Implement concurrent job queue with Redis

**Learning Focus:** Goroutines, channels, mutexes, Redis operations

### Tasks:

1. Set up Redis client connection in backend
2. Implement complete `JobQueue` struct:

- `NewJobQueue(workers int)` constructor
- `SubmitJob(job *Job)` - publish to Redis
- `GetJobStatus(jobID string)` - retrieve from Redis
- `UpdateJobStatus(jobID, status string, progress int64)`

3. Implement `StartWorkers()` with goroutines
4. Add Redis persistence for job status tracking
5. Test job submission and status retrieval

**Deliverable:** Jobs can be queued in Redis and status can be checked

---

## Day 3: Worker Service & FFmpeg Processing (10h)

**Goal:** Build worker service that processes videos

**Learning Focus:** Command execution, file I/O, worker patterns

### Tasks:

1. Build `worker/main.go` from scratch:

- Redis consumer loop
- Job polling from queue
- Connection to Redis

2. Implement `processVideo()` with FFmpeg:

- Input validation
- Progress tracking (parse FFmpeg output)
- Error handling

3. Create job processing pipeline:

- Pull job from Redis
- Process video (compress/transcode)
- Update status in Redis

4. Test local processing without MinIO

**Deliverable:** Worker consumes jobs and processes videos locally

---

## Day 4: MinIO Storage Integration (10h)

**Goal:** Store processed videos in MinIO

**Learning Focus:** S3-compatible storage, io.Reader/Writer, bucket operations

### Tasks:

1. Set up MinIO client in both backend and worker
2. Create buckets for uploads and processed videos
3. Update backend upload handler:

- Store original video in MinIO instead of local disk
- Update job with MinIO object path

4. Update worker:

- Download video from MinIO to temp location
- Process video
- Upload processed video back to MinIO
- Clean up temp files

5. Implement presigned URL generation for downloads

**Deliverable:** Full pipeline uses MinIO for storage

---

## Day 5: End-to-End Integration & Error Handling (10h)

**Goal:** Wire everything together and handle edge cases

**Learning Focus:** Error handling patterns, logging, debugging

### Tasks:

1. Fix Docker Compose configuration:

- Add Dockerfiles for backend and worker
- Configure volume mounts
- Set up health checks

2. Implement comprehensive error handling:

- Network failures
- FFmpeg errors
- File size limits
- Invalid video formats

3. Add structured logging throughout
4. Test full flow: Upload → Redis → Worker → MinIO → Status
5. Handle worker crashes and job retries

**Deliverable:** Reliable end-to-end pipeline in Docker

---

## Day 6: Download, UI Polish & Testing (10h)

**Goal:** Complete MVP with download functionality

**Learning Focus:** HTTP streaming, context management, testing

### Tasks:

1. Implement download handler in backend:

- `/download/:jobID` endpoint
- Stream from MinIO with presigned URLs
- Handle in-progress vs completed jobs

2. Update `frontend/index.html`:

- Add status polling after upload
- Show processing progress
- Add download button when complete

3. Add job listing endpoint (view all jobs)
4. Write basic tests for critical paths
5. Create README with setup instructions
6. Test full workflow with multiple concurrent uploads

**Deliverable:** Working MVP matching Phase 1 roadmap

---

## Key Files to Modify

- `backend/main.go` - Core API server (Days 1-2, 5-6)
- `worker/main.go` - Video processing worker (Days 3-4)
- `infra/docker-compose.yml` - Orchestration (Day 5)
- `frontend/index.html` - UI updates (Day 6)
- Create: `backend/go.mod`, `worker/go.mod`, Dockerfiles

## Success Criteria

- ✅ Upload video through UI
- ✅ Job queued in Redis
- ✅ Worker processes video with FFmpeg
- ✅ Processed video stored in MinIO
- ✅ Download processed video
- ✅ Runs in Docker Compose

## Learning Resources Per Day

Each day allocate 1-2 hours for:

- Reading Go documentation for day's concepts
- Experimenting with small examples
- Debugging and understanding errors

### To-dos

- [ ] Fix all syntax errors in backend/main.go and set up Go modules
- [ ] Implement working upload handler with proper error handling
- [ ] Set up Redis client and implement JobQueue with goroutines
- [ ] Add Redis persistence for job status tracking and retrieval
- [ ] Build worker service that consumes jobs from Redis
- [ ] Implement video processing with FFmpeg and progress tracking
- [ ] Set up MinIO client and create storage buckets
- [ ] Update backend and worker to use MinIO for all file storage
- [ ] Create Dockerfiles and fix docker-compose.yml configuration
- [ ] Implement comprehensive error handling and logging
- [ ] Implement download handler with MinIO presigned URLs
- [ ] Update frontend with status polling and download functionality