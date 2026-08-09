package main

import (
	"encoding/json"
	"net/http"

	"github.com/devlup-labs/Commitment-Issues/internals"
)

func getRepository(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
	repoName, err := internals.GetRepoName()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	currBranch, err := internals.GetCurrBranch()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	incomingBranch, err := internals.GetIncomingBranch()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"name":          repoName,
		"currentBranch": currBranch,
		"incomingBranch": incomingBranch,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getSuggestions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
	w.Header().Set("Content-Type", "application/json")

	details := internals.GetDetails()
	json.NewEncoder(w).Encode(details)
}

func main() {
	http.HandleFunc("/api/repository", getRepository)
	http.HandleFunc("/api/suggestions", getSuggestions)

	http.ListenAndServe(":8080", nil)
}
