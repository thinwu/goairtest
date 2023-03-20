package main

import (
	"fmt"
    "encoding/json"
    "log"
    "net/http"
)

type User struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

func main() {
    http.HandleFunc("/schemes", getSchemes)
	http.HandleFunc("/testrun", testRun)
	http.HandleFunc("/agent", agent)
    log.Fatal(http.ListenAndServe(":8899", nil))
}

func getSchemes(w http.ResponseWriter, r *http.Request) {
    users := []User{
        {Name: "John Doe", Email: "john.doe@example.com"},
        {Name: "Jane Smith", Email: "jane.smith@example.com"},
    }
    json.NewEncoder(w).Encode(users)
}
func testRun(w http.ResponseWriter, r *http.Request) {
    if r.Method == "POST" {
        // Get the POST data
        err := r.ParseForm()
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        build := r.Form.Get("build")
		testCase := r.Form.Get("testCase")
		jobName := r.Form.Get("jobName")
        // Print the POST data
        fmt.Fprintf(w, "build data: %s", build)
		fmt.Fprintf(w, "testCase data: %s", testCase)
		fmt.Fprintf(w, "jobName data: %s", jobName)
    } else {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
    }
}
func agent(w http.ResponseWriter, r *http.Request) {
    if r.Method == "POST" {
        // Get the POST data
        err := r.ParseForm()
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        port := r.Form.Get("port")
		address := r.Form.Get("address")
		hostname := r.Form.Get("hostname")
        // Print the POST data
        fmt.Fprintf(w, "port data: %s", port)
		fmt.Fprintf(w, "address data: %s", address)
		fmt.Fprintf(w, "hostname data: %s", hostname)

    } else {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
    }
}
