package main

import (
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
	"encoding/json"
	"strings"

	
)

const uploadPath = "./worker"

type Job struct{
	ID string
	status string
	inputPath string
	outputPath string
	Error error
	CreatedAt time.Time
	progress int64
}


type JobQueue struct{
	jobs  chan *Job
	status map[string]*Job
	mu 		sync.RWMutex
	workers int


}

var jobs = make(map[string]*Job)


func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/uploads", uploadFileHandler)
	mux.HandleFunc("/jobID", JobHandler)
	mux.HandleFunc("/status/", statusHandler)
	http.ListenAndServe(":8080", mux)
	
	


}



	func homeHandler(w http.ResponseWriter ,r *http.Request){
		tmpl := template.Must(template.ParseFiles("frontend/index.html"))
		tmpl.Execute(w, nil)
	}


	func uploadFileHandler(w http.ResponseWriter, r *http.Request){


		r.Body = http.MaxBytesReader(w, r.Body, 100 << 20)
		if err := r.ParseMultipartForm(100 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
				return

		}
		defer r.MultipartForm.RemoveAll()


		file,header, err := r.FormFile("video")
		if err!= nil{
			http.Error(w, err.Error(), http.StatusBadRequest)
			return

		}
		defer file.Close()

		inputPath := uploadPath + "/Input" + header.Filename
		dst ,err := os.Create(inputPath)
		if err != nil{
			http.Error(w,err.Error(), http.StatusBadRequest)
			return

		}
		defer dst.Close()

		

		_, err = io.Copy(dst, file)
		if err != nil{
			http.Error(w,err.Error(), http.StatusBadRequest)
			return
		}
		
	
		
		outPath := uploadPath+ "/output_" + header.Filename
		cmd := &exec.Cmd{
			Path: `C:\ffmpeg\bin\ffmpeg.exe`,
			Args: []string{"ffmpeg", "-i", inputPath, "-crf", "28" , outPath},

		}

		if err := cmd.Run(); err != nil{
			http.Error(w, err.Error(), 500)
			return

		}

		w.Header().Set("Content-Disposition", "attachment; filename="+outPath)
        http.ServeFile(w, r, outPath)

	}



	func JobHandler(w http.ResponseWriter, r  *http.Request){
		


	}


	func statusHandler(w http.ResponseWriter, r *http.Request){
		
    	jobID := strings.TrimPrefix(r.URL.Path, "/status/")

		if jobID == "" {
			http.Error(w, "Job ID required", http.StatusBadRequest)
			return


			}	
		job,exists := jobs[jobID]
		if !exists{
			http.Error(w, "Job Not Found", http.StatusNotFound)
			return

			}
			json.NewEncoder(w).Encode(job)
		

		
	}
	

	
