package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	URLs []string `yaml:"urls"`
}

type Stats struct {
	URL       string
	SizeBytes int64
	Elapsed   time.Duration
	SpeedMBps float64
	SpeedMbps float64
	Error     error
}

func downloadAndMeasure(ctx context.Context, url string) <-chan Stats {
	ch := make(chan Stats)

	go func() {
		defer close(ch)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		resp, err := client.Get(url)
		if err != nil {
			ch <- Stats{URL: url, Error: err}
			return
		}
		defer resp.Body.Close()

		var downloaded int64
		start := time.Now()
		buf := make([]byte, 32*1024)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Printf("Download took %v \n", time.Since(start))
				return
			case <-ticker.C:
				elapsed := time.Since(start)
				ch <- Stats{
					URL:       url,
					SizeBytes: downloaded,
					Elapsed:   elapsed,
					SpeedMBps: float64(downloaded) / 1e6 / elapsed.Seconds(),
					SpeedMbps: float64(downloaded*8) / 1e6 / elapsed.Seconds(),
				}
			default:
				n, err := resp.Body.Read(buf)
				if n > 0 {
					downloaded += int64(n)
				}
				if err == io.EOF {
					elapsed := time.Since(start)
					ch <- Stats{
						URL:       url,
						SizeBytes: downloaded,
						Elapsed:   elapsed,
						SpeedMBps: float64(downloaded) / 1e6 / elapsed.Seconds(),
						SpeedMbps: float64(downloaded*8) / 1e6 / elapsed.Seconds(),
					}
					return
				}
				if err != nil {
					ch <- Stats{URL: url, Error: err}
					return
				}
			}
		}
	}()

	return ch
}

func main() {
	raw, err := os.ReadFile("urls.yaml")
	if err != nil {
		log.Fatal(err)
	}

	var config Config
	if err := yaml.Unmarshal(raw, &config); err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure resources are released when the function exits

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		<-signalChan
		fmt.Println("\nReceived interrupt signal. Shutting down...")
		cancel()
	}()

	for {
		for _, url := range config.URLs {
			select {
			case <-ctx.Done():
				return
			case result := <-downloadAndMeasure(ctx, url):
				fmt.Printf("✓ %s\n", result.URL)
				fmt.Printf("  Size:     %.2f MB\n", float64(result.SizeBytes)/1e6)
				fmt.Printf("  Time:     %v\n", result.Elapsed)
				fmt.Printf("  Speed:    %.2f MB/s (%.2f Mbps)\n\n", result.SpeedMBps, result.SpeedMbps)
			}
		}
	}
}
