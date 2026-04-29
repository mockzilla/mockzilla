package main

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/contexts"
)

// main generates a list of available fake functions for documentation.
func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting current working directory: %v", err)
	}
	docsDir := filepath.Join(cwd, "docs")
	outputFile := filepath.Join(docsDir, "fake-list.md")

	existingContent, err := os.ReadFile(outputFile)
	if err != nil {
		log.Fatalf("Error reading the file: %v", err)
	}

	title := "## Aliases"

	var sb strings.Builder
	sb.WriteString("```\n")

	names := make([]string, 0, len(contexts.ContextFunctions0Arg))
	for name := range contexts.ContextFunctions0Arg {
		names = append(names, "fake:"+name)
	}
	sort.Strings(names)

	for _, name := range names {
		sb.WriteString(name + "\n")
	}

	sb.WriteString("```\n")

	newContent := sb.String()

	// Check if the existing content contains "## Aliases"
	contentLines := strings.Split(string(existingContent), "\n")
	lineIndex := -1

	// Find the index of "## Aliases"
	for i, line := range contentLines {
		if line == title {
			lineIndex = i
			break
		}
	}

	if lineIndex != -1 {
		// If "## Aliases" is found, replace from that line onward
		contentLines = contentLines[:lineIndex+1]
		contentLines = append(contentLines, newContent)
	} else {
		// append at the end
		contentLines = append(contentLines, title+"\n"+newContent)
	}

	updatedContent := strings.Join(contentLines, "\n")

	// Write the updated content back to the Markdown file
	if err := os.WriteFile(outputFile, []byte(updatedContent), os.ModePerm); err != nil {
		log.Fatal(err)
	}

	log.Printf("Fake function list of %d aliases appended to %s!", len(names), outputFile)
}
