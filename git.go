package main

import(
	"os/exec"
	"bytes"
)

//function to get conflictedFiles
func getConflictedFiles() ([]string, error){
	//prepare the git command
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")

	//Execute the command and capture its output
	output, err := cmd.Output()
	if err != nil{
		return nil, err
	}

	//Remove extra spaces/newlines and split output into lines
	lines := bytes.Split(bytes.TrimSpace(output), []byte("\n"))

	var files []string

	//convert each line from []byte to string
	for _, line := range lines{
		if len(line) > 0{
			files = append(files, string(line))
		}
	}

	return files, nil
}

//to get the conflicted file of ourBranch
func getOursVersion(file string)([]byte, error){
	cmd := exec.Command("git", "show", ":2:"+file)
	return cmd.Output()
}

//to get conflicted file of incomingBranch
func getTheirVersion(file string)([]byte, error){
	cmd := exec.Command("git", "show", ":3:"+file)
	return cmd.Output()
}