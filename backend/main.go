package main

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"github.com/google/uuid"
)

const uploadPath = "./worker"

type Job struct {
	ID         string `json:"job_id"`
	Status     string	`json:"status"`
	InputPath  string	`json:"-"`
	OutputPath string	`json:"-"`
	Error      error	`json:"error,omitempty"`
	CreatedAt  time.Time	`json:"created_at"`
	Progress   int64	`json:"progess"`
	OriginalFilename string `json:"original_filename"`
}

type JobQueue struct {
	jobs    chan *Job
	status  map[string]*Job
	mu      sync.RWMutex
	Workers int
}


var jobs = make(map[string]*Job)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/jobID", JobHandler)
	mux.HandleFunc("/status/", statusHandler)
	http.ListenAndServe(":8080", mux)

}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("frontend/index.html"))
	tmpl.Execute(w, nil)
}

func(j *Job) uploadFileHandler() {

	job := &Job{
		ID:	uuid.New().String(),   
		Status:	"Pending",
		InputPath: inputPath,
		OutputPath: outPath,
		CreatedAt: time.Now(),
		Progress: 0,
		OriginalFilename: header.Filename,

	}
	

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return

	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return

	}
	defer file.Close()

	inputPath := uploadPath + "/Input" + header.Filename
	dst, err := os.Create(inputPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return

	}
	defer dst.Close()

	_, err := io.Copy(dst, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}


	outPath := uploadPath + "/output" + header.Filename

	
	jobQueue.SubmitJob(job)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "Pending",

	})
	
}



func JobHandler(w http.ResponseWriter, r *http.Request) {

}

func statusHandler(w http.ResponseWriter, r *http.Request) {

	jobID := strings.TrimPrefix(r.URL.Path, "/status/")

	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return

	}
	job, exists := jobs[jobID]
	if !exists {
		http.Error(w, "Job Not Found", http.StatusNotFound)
		return

	}
	json.NewEncoder(w).Encode(job)

}


func NewJobQueue(workers int) *JobQueue{
	ch:= make(chan *Job, 100)
	jobStatus := make(map[string]*Job)
	


}


func submitJob (job *Job){
	

}

func GetJobStatus(jobID string) *Job{



}


func ( jq *JobQueue) updateJobStatus(status string, jobID string, progress int64){


}
m

 for i := 0; i < jq.Workers; i++ {
	go jq.worker()


 }

}


func (jq *JobQueue) worker(){
	job := <-jq.jobs
	for job := range jq.jobs {
		jq.UpdateJobStatus(job.ID, "Processing", 0)
		
		err := processVideo(job.InputPath, job.OutputPath)
		if err != nil {
			jq.UpdateJobStatus(job.ID, "Failed!", 0)
		} else {
				jq.UpdateJobStatus(job.ID, "Success!", 100)


		}


	}
			




}


func processVideo(inputPath, outPath string) error {
	cmd := exec.Command(
		"C:\\ffmpeg\\bin\\ffmpeg.exe",
		"-i", inputPath,                 
        "-crf", "28",                    
        outPath,  
	)

	err := cmd.Run()
	return err
}
































