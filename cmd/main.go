package main

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/chewycrunch/shopify-monitor/shopify"
)

// Fetch variants and load them into the map

// Compare variants to the map, see if any availabilty has changed, or new variants exist
// If so, send a webhook to the webhook URL

type Config struct {
	Delay int `json:"delay"`
}

var config Config

func loadConfig() (Config, error) {

	// Read the config file
	file, err := os.Open("config/config.json")
	if err != nil {
		return config, err
	}

	// Parse the JSON config
	// Read the contents of the file into a byte slice
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return config, err
	}

	// Parse the JSON config
	err = json.Unmarshal(fileBytes, &config)
	if err != nil {
		return config, err
	}
	return config, nil
}

func init() {
	inconfig, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	config = inconfig
}

// Read config/websites.csv
func main() {
	log.Println("Welcome to the Shopify Monitor")

	file, err := os.Open("config/websites.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Parse the CSV file
	reader := csv.NewReader(file)
	_, err = reader.Read() // Disregard the header line
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup

	// Process each website
	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatal(err)
		}

		log.Printf("Starting %v", record[0])

		websiteURL := record[0]
		webhookURL := record[1]

		// Add to the wait group
		wg.Add(1)

		// Start a goroutine for each website
		go func() {
			defer wg.Done()
			monitor := shopify.NewMonitor(websiteURL, webhookURL)
			monitor.InitializeVariants()
			monitor.StartWatching(time.Duration(config.Delay) * time.Millisecond)
		}()
	}

	// Wait for all goroutines to finish
	wg.Wait()
}
