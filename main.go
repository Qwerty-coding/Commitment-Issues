package main

import (
	"fmt"
	"os"
	"path/filepath"
)

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

		ours, err := getOursVersion(file)
		if err != nil {
			fmt.Println("Error getting ours version", err)
			continue
		}

		theirs, err := getTheirVersion(file)
		if err != nil {
			fmt.Println("Error getting their version", err)

			continue
		}

		base := filepath.Base(file)

		err = writeToFiles("ours"+base, ours)
		if(err != nil){
			fmt.Println("Error occured while writting the file")
		}

		err = writeToFiles("theirs"+base,theirs)
		if(err != nil){
			fmt.Println("Error occured while writting the file")
		}
		fmt.Println("files written successfulllllly")

		//get the ast of the files and srcCode of the file
		ast, srcCode := getAST(filepath.Join("testfiles", "ours"+base))
		fmt.Println(ast.String())
		fmt.Println(string(srcCode))

		ast, srcCode = getAST(filepath.Join("testfiles", "theirs"+base))
		fmt.Println(ast.String())
		fmt.Println(string(srcCode))

		//using Smackeer
		runSmacker(base)
	}
}


