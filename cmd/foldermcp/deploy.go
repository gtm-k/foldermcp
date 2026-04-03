package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy <target>",
	Short: "Generate deployment artifacts",
	Long: `Generates deployment configuration files for the specified target.
Currently supports: docker, cloudrun.`,
	Args: cobra.ExactArgs(1),
	RunE: runDeploy,
}

func init() {
	deployCmd.Flags().Bool("dry-run", false, "print generated files to stdout instead of writing")
	rootCmd.AddCommand(deployCmd)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	target := args[0]
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	switch target {
	case "docker":
		return deployDocker(dryRun)
	case "cloudrun":
		return deployCloudRun(dryRun)
	default:
		return fmt.Errorf("unsupported target %q; supported: docker, cloudrun", target)
	}
}

func deployDocker(dryRun bool) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	dockerfile := `# syntax=docker/dockerfile:1
# Multi-stage build for FolderMCP

# --- Stage 1: Build the Go binary ---
FROM golang:1.25.0 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /foldermcp ./cmd/foldermcp

# --- Stage 2: Runtime ---
FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

# Install uv for fast Python dependency management.
RUN pip install uv

# Copy the built binary.
COPY --from=builder /foldermcp /usr/local/bin/foldermcp

# Copy workspace files.
WORKDIR /workspace
COPY . .

# Initialize and serve.
ENTRYPOINT ["foldermcp"]
CMD ["serve", "--mode=production"]
`

	dockerCompose := `version: "3.8"
services:
  foldermcp:
    build: .
    stdin_open: true
    volumes:
      - .:/workspace
    working_dir: /workspace
`

	if dryRun {
		fmt.Println("# --- Dockerfile ---")
		fmt.Print(dockerfile)
		fmt.Println()
		fmt.Println("# --- docker-compose.yml ---")
		fmt.Print(dockerCompose)
		return nil
	}

	// Write files.
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", dockerfilePath)

	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("write docker-compose.yml: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", composePath)

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Build and run:")
	fmt.Fprintln(os.Stderr, "  docker compose build")
	fmt.Fprintln(os.Stderr, "  docker compose run --rm foldermcp init .")
	fmt.Fprintln(os.Stderr, "  docker compose run --rm foldermcp review --approve=<tools>")
	fmt.Fprintln(os.Stderr, "  docker compose up")

	return nil
}

func deployCloudRun(dryRun bool) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	serviceYAML := `apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: foldermcp
spec:
  template:
    spec:
      containers:
        - image: gcr.io/PROJECT_ID/foldermcp
          ports:
            - containerPort: 3000
          env:
            - name: FOLDERMCP_MODE
              value: team
`

	deployScript := `#!/bin/bash
set -euo pipefail
# Deploy FolderMCP to Cloud Run.
# Set PROJECT_ID before running: export PROJECT_ID=my-gcp-project

if [ -z "${PROJECT_ID:-}" ]; then
  echo "Error: PROJECT_ID environment variable is not set." >&2
  exit 1
fi

echo "Building and pushing container image..."
gcloud builds submit --tag gcr.io/$PROJECT_ID/foldermcp

echo "Deploying to Cloud Run..."
gcloud run deploy foldermcp --image gcr.io/$PROJECT_ID/foldermcp --platform managed --port 3000

echo "Deployment complete."
`

	if dryRun {
		fmt.Println("# --- service.yaml ---")
		fmt.Print(serviceYAML)
		fmt.Println()
		fmt.Println("# --- deploy-cloudrun.sh ---")
		fmt.Print(deployScript)
		return nil
	}

	// Write files.
	serviceYAMLPath := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(serviceYAMLPath, []byte(serviceYAML), 0644); err != nil {
		return fmt.Errorf("write service.yaml: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", serviceYAMLPath)

	deployScriptPath := filepath.Join(dir, "deploy-cloudrun.sh")
	if err := os.WriteFile(deployScriptPath, []byte(deployScript), 0755); err != nil {
		return fmt.Errorf("write deploy-cloudrun.sh: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", deployScriptPath)

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Deploy to Cloud Run:")
	fmt.Fprintln(os.Stderr, "  export PROJECT_ID=my-gcp-project")
	fmt.Fprintln(os.Stderr, "  bash deploy-cloudrun.sh")

	return nil
}
