package integrationtest

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

const (
	defaultBatchSizeBytes = 6 * 1024 * 1024 // 6MB
	defaultBatchMaxFiles  = 20
)

//go:embed templates/server.tmpl
var batchServerTemplate []byte

// SplitIntoBatches splits specs into batches based on target size in bytes.
// Each batch will contain specs until either target size or max files is reached.
// Large files (>= 50% of target) get their own batch for isolation.
func SplitIntoBatches(specs []string, targetBytes int64) [][]string {
	if targetBytes <= 0 {
		targetBytes = defaultBatchSizeBytes
	}

	largeFileThreshold := targetBytes / 2

	// Get file sizes
	type specWithSize struct {
		path string
		size int64
	}
	items := make([]specWithSize, 0, len(specs))
	for _, spec := range specs {
		size := int64(0)
		if info, err := os.Stat(spec); err == nil {
			size = info.Size()
		}
		items = append(items, specWithSize{spec, size})
	}

	// Sort by size descending - large files first for better packing
	sort.Slice(items, func(i, j int) bool {
		return items[i].size > items[j].size
	})

	// Separate large files (each gets own batch) from regular files
	var batches [][]string
	var batchSizes []int64
	var regularItems []specWithSize

	for _, item := range items {
		if item.size >= largeFileThreshold {
			// Large file gets its own batch
			batches = append(batches, []string{item.path})
			batchSizes = append(batchSizes, item.size)
		} else {
			regularItems = append(regularItems, item)
		}
	}

	// Greedy bin-packing for regular files (constrained by size AND file count)
	for _, item := range regularItems {
		placed := false
		for i := range batches {
			if batchSizes[i]+item.size <= targetBytes && len(batches[i]) < defaultBatchMaxFiles {
				batches[i] = append(batches[i], item.path)
				batchSizes[i] += item.size
				placed = true
				break
			}
		}
		if !placed {
			// Start new batch
			batches = append(batches, []string{item.path})
			batchSizes = append(batchSizes, item.size)
		}
	}

	return batches
}

// BatchServerInfo holds info about a built batch server
type BatchServerInfo struct {
	BatchID      int
	ServiceNames []string
	ServerBin    string
	ServerDir    string
}

// GenerateBatchServer generates a main.go that imports all services in the batch
func GenerateBatchServer(sandboxDir string, batchID int, serviceNames []string) (*BatchServerInfo, error) {
	// Get module name for imports
	moduleName := "github.com/mockzilla/mockzilla/v2"

	// Build service imports using relative services path
	paths := config.NewPaths("")
	var serviceImports []string
	for _, name := range serviceNames {
		importPath := fmt.Sprintf("%s/%s/%s", moduleName, paths.Services, name)
		serviceImports = append(serviceImports, importPath)
	}

	// Generate main.go content
	tmpl, err := template.New("batch").Parse(string(batchServerTemplate))
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	data := struct {
		ServiceImports []string
		ServiceCount   int
	}{
		ServiceImports: serviceImports,
		ServiceCount:   len(serviceNames),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	// Create batch server directory
	batchDir := filepath.Join(sandboxDir, fmt.Sprintf(".batch-%d", batchID))
	if err := os.MkdirAll(batchDir, 0755); err != nil {
		return nil, fmt.Errorf("creating batch dir: %w", err)
	}

	// Write main.go
	mainPath := filepath.Join(batchDir, "main.go")
	if err := os.WriteFile(mainPath, buf.Bytes(), 0644); err != nil {
		return nil, fmt.Errorf("writing main.go: %w", err)
	}

	return &BatchServerInfo{
		BatchID:      batchID,
		ServiceNames: serviceNames,
		ServerDir:    batchDir,
	}, nil
}

// BuildBatchServer builds the batch server binary
func BuildBatchServer(sandboxDir string, info *BatchServerInfo) error {
	serverBin := filepath.Join(sandboxDir, fmt.Sprintf("batch-server-%d", info.BatchID))

	cmd := exec.Command("go", "build", "-o", serverBin, ".")
	cmd.Dir = info.ServerDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w\nstderr: %s", err, stderr.String())
	}

	info.ServerBin = serverBin
	return nil
}
