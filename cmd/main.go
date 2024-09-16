package main

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/chewycrunch/shopify-monitor/monitor"
	"github.com/chewycrunch/shopify-monitor/proxy"
)

// Fetch variants and load them into the map

// Compare variants to the map, see if any availabilty has changed, or new variants exist
// If so, send a webhook to the webhook URL
var wg sync.WaitGroup

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

func startMonitorService(wg *sync.WaitGroup, proxyManager *proxy.ProxyManager) {
	defer wg.Done()
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

	// Process each website
	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatal(err)
		}

		websiteURL := record[0]
		webhookURL := record[1]

		// Add to the wait group
		wg.Add(1)

		// Start a goroutine for each website
		go func() {
			defer wg.Done()
			monitor := monitor.NewMonitor(websiteURL, webhookURL, proxyManager)
			monitor.InitializeVariants()
			monitor.StartWatching(time.Duration(config.Delay) * time.Millisecond)
		}()

	}
}

// Read config/websites.csv
func main() {
	log.Println("Welcome to the Shopify Monitor")

	// Initialize the proxy manager once and share it
	proxyFile, err := os.Open("config/proxies.txt")
	if err != nil {
		log.Fatalf("Failed to open proxy file: %v", err)
	}
	defer proxyFile.Close()

	shopifyProxyBroker := proxy.NewProxyManager(50)
	shopifyProxyBroker.LoadProxiesFromFile(proxyFile)

	webhookProxyBroker := proxy.NewProxyManager(50)
	webhookProxyBroker.LoadProxiesFromFile(proxyFile)

	wg.Add(1)
	go startMonitorService(&wg, shopifyProxyBroker)

	wg.Wait()
}
