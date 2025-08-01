package main

import (
	"bytes"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/jedib0t/go-pretty/v6/table"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"howett.net/plist"
)

// NSDate epoch: January 1, 2001 00:00:00 UTC
var nsDateEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// NotificationData represents the structure of the notification data
type NotificationData struct {
	App  string  `plist:"app"`
	Date float64 `plist:"date"`
	Req  Request `plist:"req"`
	Srce []byte  `plist:"srce"`
	UUID []byte  `plist:"uuid"`
}

// Request represents the request part of the notification
type Request struct {
	Body string `plist:"body"`
	Iden string `plist:"iden"`
	Soun Soun   `plist:"soun"`
	Subt string `plist:"subt"`
	Titl string `plist:"titl"`
}

// Soun represents the sound structure
type Soun struct {
	Nam string `plist:"nam"`
}

// const dbFile = "./db.sqlite"
const dbFilePath = "Library/Group Containers/group.com.apple.usernoted/db2/db"

// UsernameMapping stores the mapping of real usernames to generated names
type UsernameMapping struct {
	realToGenerated map[string]string
	generatedToReal map[string]string
}

// NewUsernameMapping creates a new username mapping
func NewUsernameMapping() *UsernameMapping {
	return &UsernameMapping{
		realToGenerated: make(map[string]string),
		generatedToReal: make(map[string]string),
	}
}

// GetGeneratedName returns a generated name for the given real username
func (um *UsernameMapping) GetGeneratedName(realName string) string {
	if generated, exists := um.realToGenerated[realName]; exists {
		return generated
	}

	// Generate a new random name
	generated := petname.Generate(2, " ")

	// Ensure uniqueness
	for {
		if _, exists := um.generatedToReal[generated]; !exists {
			break
		}
		generated = petname.Generate(2, " ")
	}

	// Uppercase the first letter of each word
	caser := cases.Title(language.English)
	generated = caser.String(generated)

	um.realToGenerated[realName] = generated
	um.generatedToReal[generated] = realName

	return generated
}

// ReplaceUsernamesInText replaces usernames in text with generated names
func (um *UsernameMapping) ReplaceUsernamesInText(text string) string {
	if text == "" {
		return text
	}

	if !strings.HasPrefix(text, "#") {
		text = um.GetGeneratedName(text)
	}

	return text
}

func main() {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Parse command line flags
	replaceUsernames := flag.Bool("replace-user-name", false, "Replace usernames with randomly generated names for privacy")
	flag.Parse()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("sqlite3", homeDir+"/"+dbFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("select app_id, data from record")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	// Map to store daily notification counts
	dailyCounts := make(map[string]int)
	// Map to store channel notification counts
	channelCounts := make(map[string]int)
	totalSlackNotifications := 0

	// Initialize username mapping if needed
	var usernameMapping *UsernameMapping
	if *replaceUsernames {
		usernameMapping = NewUsernameMapping()
	}

	for rows.Next() {
		var app_id int
		var data []byte
		err = rows.Scan(&app_id, &data)
		if err != nil {
			log.Fatal(err)
		}

		// Parse the plist data
		var notificationData NotificationData
		decoder := plist.NewDecoder(bytes.NewReader(data))
		err := decoder.Decode(&notificationData)
		if err != nil {
			fmt.Printf("Error decoding plist for app_id %d: %v\n", app_id, err)
			continue
		}

		// Filter for Slack notifications only
		if notificationData.App == "com.tinyspeck.slackmacgap" {
			totalSlackNotifications++

			// Convert NSDate timestamp to date
			// NSDate uses seconds since January 1, 2001 (Core Data epoch)
			timestamp := nsDateEpoch.Add(time.Duration(notificationData.Date) * time.Second)
			dateStr := timestamp.Format("2006-01-02")

			// Increment count for this date
			dailyCounts[dateStr]++

			// Increment count for this channel
			channel := notificationData.Req.Subt
			if channel == "" {
				channel = "Unknown"
			}

			// Replace usernames in channel name if flag is enabled
			if *replaceUsernames && usernameMapping != nil {
				channel = usernameMapping.ReplaceUsernamesInText(channel)
			}

			channelCounts[channel]++
		}
	}

	// Print results
	fmt.Printf("Total Slack notifications found: %d\n\n", totalSlackNotifications)

	// Create daily counts table
	if len(dailyCounts) > 0 {
		fmt.Println("Daily Slack notification counts:")
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"Date", "Count"})

		// Sort dates for consistent output
		var dates []string
		for date := range dailyCounts {
			dates = append(dates, date)
		}
		sort.Strings(dates)

		for _, date := range dates {
			t.AppendRow(table.Row{date, dailyCounts[date]})
		}

		t.SetStyle(table.StyleColoredDark)
		t.Render()
		fmt.Println()
	}

	// Create channel counts table
	if len(channelCounts) > 0 {
		fmt.Println("Slack channel notification counts:")
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"Channel", "Count"})

		// Sort channels by count (descending) for better readability
		type channelCount struct {
			channel string
			count   int
		}
		var sortedChannels []channelCount
		for channel, count := range channelCounts {
			sortedChannels = append(sortedChannels, channelCount{channel, count})
		}
		sort.Slice(sortedChannels, func(i, j int) bool {
			return sortedChannels[i].count > sortedChannels[j].count
		})

		for _, cc := range sortedChannels {
			t.AppendRow(table.Row{cc.channel, cc.count})
		}

		t.SetStyle(table.StyleColoredDark)
		t.Render()
	}
}
