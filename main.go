package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	indexURL  = "https://chaos-data.projectdiscovery.io/index.json"
	cacheFile = "index.json"
	chaosDir  = "chaos"
)

type Program struct {
	Name  string `json:"name"`
	URL   string `json:"URL"`
	Count int    `json:"count"`
}

type downloadResult struct {
	program Program
	zipPath string
	err     error
}

type unzipJob struct {
	program Program
	zipPath string
}

type queryResult struct {
	file       string
	matchCount int
}

func main() {
	refresh := flag.Bool("refresh", false, "Refresh the index.json cache")
	download := flag.String("dl", "", "Download subdomains for a specific program (or 'all')")
	query := flag.String("q", "", "Query for a domain across all downloaded data")
	list := flag.Bool("list", false, "List all available programs")
	workers := flag.Int("w", runtime.NumCPU()*2, "Number of concurrent workers")
	flag.Parse()

	if *refresh || !fileExists(cacheFile) {
		fmt.Println("[*] Fetching index.json...")
		if err := fetchIndex(); err != nil {
			fmt.Fprintf(os.Stderr, "[-] Error fetching index: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[+] Index cached")
	}

	programs, err := loadIndex()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Error loading index: %v\n", err)
		os.Exit(1)
	}

	switch {
	case *list:
		for _, p := range programs {
			fmt.Println(p.Name)
		}
	case *download != "":
		parallelDownload(programs, *download, *workers)
	case *query != "":
		parallelQuery(*query, *workers)
	default:
		flag.Usage()
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fetchIndex() error {
	resp, err := http.Get(indexURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(cacheFile)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func loadIndex() ([]Program, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}

	var programs []Program
	if err := json.Unmarshal(data, &programs); err != nil {
		return nil, err
	}
	return programs, nil
}

func parallelDownload(programs []Program, target string, workers int) {
	var toDownload []Program

	if target == "all" {
		toDownload = programs
	} else {
		for _, p := range programs {
			if strings.EqualFold(p.Name, target) {
				toDownload = append(toDownload, p)
				break
			}
		}
		if len(toDownload) == 0 {
			fmt.Fprintf(os.Stderr, "[-] Program '%s' not found\n", target)
			os.Exit(1)
		}
	}

	os.MkdirAll(chaosDir, 0755)

	// Stage 1: Parallel downloads
	fmt.Printf("[*] Downloading %d programs with %d workers...\n", len(toDownload), workers)

	downloadJobs := make(chan Program, len(toDownload))
	downloadResults := make(chan downloadResult, len(toDownload))

	// Start download workers
	var dlWg sync.WaitGroup
	for i := 0; i < workers; i++ {
		dlWg.Add(1)
		go func() {
			defer dlWg.Done()
			for p := range downloadJobs {
				zipPath, err := downloadZip(p)
				downloadResults <- downloadResult{program: p, zipPath: zipPath, err: err}
			}
		}()
	}

	// Feed download jobs
	go func() {
		for _, p := range toDownload {
			if p.URL != "" && p.Count > 0 {
				downloadJobs <- p
			}
		}
		close(downloadJobs)
	}()

	// Close results when downloads complete
	go func() {
		dlWg.Wait()
		close(downloadResults)
	}()

	// Stage 2: Parallel unzip (pipeline from downloads)
	unzipJobs := make(chan unzipJob, workers*2)
	var unzipWg sync.WaitGroup

	// Start unzip workers
	for i := 0; i < workers; i++ {
		unzipWg.Add(1)
		go func() {
			defer unzipWg.Done()
			for job := range unzipJobs {
				destDir := filepath.Join(chaosDir, job.program.Name)
				os.MkdirAll(destDir, 0755)

				if err := unzip(job.zipPath, destDir); err != nil {
					fmt.Fprintf(os.Stderr, "[-] Unzip %s: %v\n", job.program.Name, err)
				} else {
					fmt.Printf("[+] %s\n", job.program.Name)
				}
				os.Remove(job.zipPath)
			}
		}()
	}

	// Collect download results and feed to unzip
	var successCount, failCount int
	for result := range downloadResults {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "[-] Download %s: %v\n", result.program.Name, result.err)
			failCount++
			continue
		}
		successCount++
		unzipJobs <- unzipJob{program: result.program, zipPath: result.zipPath}
	}
	close(unzipJobs)
	unzipWg.Wait()

	fmt.Printf("[*] Complete: %d success, %d failed\n", successCount, failCount)
}

func downloadZip(p Program) (string, error) {
	resp, err := http.Get(p.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "chaos-*.zip")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", err
	}
	tmpFile.Close()

	return tmpPath, nil
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	// Create single output file for all subdomains
	outPath := filepath.Join(dest, "subdomains.txt")
	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	for _, f := range r.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".txt") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(writer, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func parallelQuery(domain string, workers int) {
	domain = strings.ToLower(domain)

	// Collect all txt files
	var files []string
	filepath.Walk(chaosDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, "subdomains.txt") {
			files = append(files, path)
		}
		return nil
	})

	if len(files) == 0 {
		return
	}

	// File jobs channel
	fileJobs := make(chan string, len(files))
	results := make(chan queryResult, workers)

	// Start query workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileJobs {
				count := countMatches(path, domain)
				if count > 0 {
					results <- queryResult{file: path, matchCount: count}
				}
			}
		}()
	}

	// Feed file jobs
	go func() {
		for _, f := range files {
			fileJobs <- f
		}
		close(fileJobs)
	}()

	// Close results when workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Find best match
	var best queryResult
	for result := range results {
		if result.matchCount > best.matchCount {
			best = result
		}
	}

	if best.file == "" {
		return
	}

	// Output the subdomains.txt contents
	f, err := os.Open(best.file)
	if err != nil {
		return
	}
	defer f.Close()
	io.Copy(os.Stdout, f)
}

func countMatches(path, domain string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		if strings.Contains(strings.ToLower(scanner.Text()), domain) {
			count++
		}
	}
	return count
}