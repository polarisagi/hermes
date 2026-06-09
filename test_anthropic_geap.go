package main

import (
	"fmt"
	"strings"
)

func main() {
	targetEndpointURL := "https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/google"
	publisher := "anthropic"
	subpath := "models/claude-3-haiku@20240307:rawPredict"
	projectID := "my-project"
	location := "us-east5"

	tmpl := strings.TrimSuffix(targetEndpointURL, "/")
	if tmpl == "" {
		tmpl = "https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{location}/publishers/" + publisher + "/{subpath}"
	}

	url := strings.ReplaceAll(tmpl, "{project_id}", projectID)
	url = strings.ReplaceAll(url, "{location}", location)
	url = strings.ReplaceAll(url, "{region}", location)
	url = strings.ReplaceAll(url, "{subpath}", subpath)

	fmt.Println("Generated URL:", url)
}
