package main

import (
	"fmt"
	"os"
	"os/exec"
	"bytes"
)

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

func main(){
	fmt.Println("========MergeSolver======")
	fmt.Println("searching for merge conflict...")
	//get all the conflicted files
	files,err := getConflictedFiles()

	if err!= nil {
		fmt.Println("Error : ", err)
		os.Exit(1)
	}

	//if there are no conflicted files
	if len(files) == 0{
		fmt.Println("No merge conflicts found!")
		return
	}

	//merge conflicts found
	fmt.Println("Conflicted files found : ")

	for _, file := range files{
		fmt.Println("-", file)
	}
}



// func main(){
// 	fmt.Println("Happyy Birthday to meee");
// }