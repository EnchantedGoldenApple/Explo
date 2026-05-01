package downloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"explo/src/models"
	"explo/src/util"
)

type Deezer struct {
	URL        string
	httpClient *util.HttpClient
}

func NewDeezer(deezerURL string, httpClient *util.HttpClient) *Deezer {
	if deezerURL == "" {
		slog.Warn("deezer URL is empty, deezer downloader will not be available")
	}
	return &Deezer{
		URL:        deezerURL,
		httpClient: httpClient,
	}
}


func (d *Deezer) QueryTrack(track *models.Track) error {
	if d.URL == "" {
		return fmt.Errorf("deezer URL not configured")
	}

	query := fmt.Sprintf("%s %s", track.Title, track.Artist)
	results, err := d.search(query, "track")
	if err != nil {
		return fmt.Errorf("deezer search failed for '%s': %w", query, err)
	}

	if len(results) == 0 {
		return fmt.Errorf("no results found on Deezer for '%s'", query)
	}

	if result, ok := results[0].(map[string]interface{}); ok {
		var trackID string

		if id, ok := result["id"].(string); ok {
			trackID = id
		} else if id, ok := result["id"].(float64); ok {
			trackID = fmt.Sprintf("%d", int64(id))
		} else {
			return fmt.Errorf("invalid search result format from Deezer for '%s': id field has unexpected type", query)
		}

		track.ID = trackID
		slog.Debug("found deezer track", "query", query, "track_id", track.ID)
		return nil
	}

	return fmt.Errorf("invalid search result format from Deezer for '%s'", query)
}


func (d *Deezer) GetTrack(track *models.Track) error {
	if d.URL == "" {
		return fmt.Errorf("deezer URL not configured")
	}

	if track.ID == "" {
		return fmt.Errorf("track ID not set, QueryTrack must be called first")
	}

	var musicID int64
	_, err := fmt.Sscanf(track.ID, "%d", &musicID)
	if err != nil {
		return fmt.Errorf("invalid track ID format: %s", track.ID)
	}

	taskID, err := d.download(musicID, "track", false, false)
	if err != nil {
		return fmt.Errorf("deezer download failed for track %d: %w", musicID, err)
	}

	slog.Info("deezer download enqueued", "track_id", track.ID, "task_id", taskID)
	track.File = fmt.Sprintf("deezer_%s", track.ID)
	track.Present = true // Mark as present since it's enqueued for download

	return nil
}

func (d *Deezer) search(query, searchType string) ([]interface{}, error) {
	payload := map[string]interface{}{
		"query": query,
		"type":  searchType,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	searchURL := fmt.Sprintf("%s/search", strings.TrimRight(d.URL, "/"))
	body, err := d.httpClient.MakeRequest("POST", searchURL, bytes.NewBuffer(jsonData), nil)
	if err != nil {
		slog.Error("deezer search request failed", "url", searchURL, "error", err.Error())
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	var results []interface{}
	if err := json.Unmarshal(body, &results); err != nil {
		slog.Error("failed to decode search response", "response", string(body), "error", err)
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	return results, nil
}


func (d *Deezer) download(musicID int64, downloadType string, addToPlaylist, createZip bool) (interface{}, error) {
	payload := map[string]interface{}{
		"music_id":        musicID,
		"type":            downloadType,
		"add_to_playlist": addToPlaylist,
		"create_zip":      createZip,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal download request: %w", err)
	}

	downloadURL := fmt.Sprintf("%s/download", strings.TrimRight(d.URL, "/"))
	body, err := d.httpClient.MakeRequest("POST", downloadURL, bytes.NewBuffer(jsonData), nil)
	if err != nil {
		slog.Error("deezer download request failed", "url", downloadURL, "music_id", musicID, "error", err.Error())
		return nil, fmt.Errorf("download request failed: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("failed to decode download response", "response", string(body), "error", err)
		return nil, fmt.Errorf("failed to decode download response: %w", err)
	}

	if taskID, ok := result["task_id"]; ok {
		return taskID, nil
	}

	slog.Error("no task_id in download response", "response", result)
	return nil, fmt.Errorf("no task_id in download response")
}

func (d *Deezer) GetDownloadStatus(tracks []*models.Track) (map[string]FileStatus, error) {
	if d.URL == "" {
		return make(map[string]FileStatus), nil
	}

	queueURL := fmt.Sprintf("%s/queue", strings.TrimRight(d.URL, "/"))
	body, err := d.httpClient.MakeRequest("GET", queueURL, nil, nil)
	if err != nil {
		slog.Error("deezer queue request failed", "url", queueURL, "error", err.Error())
		return make(map[string]FileStatus), nil
	}

	var queueItems []map[string]interface{}
	if err := json.Unmarshal(body, &queueItems); err != nil {
		slog.Error("failed to decode queue response", "response", string(body), "error", err)
		return make(map[string]FileStatus), nil
	}

	statuses := make(map[string]FileStatus)
	for _, item := range queueItems {
		filename, _ := item["filename"].(string)
		state, _ := item["state"].(string)
		id, _ := item["id"].(string)

		if filename == "" {
			continue
		}

		percentComplete := 0.0
		if state == "completed" || strings.Contains(state, "Success") {
			percentComplete = 100.0
		}

		statuses[filename] = FileStatus{
			ID:              id,
			Filename:        filename,
			State:           state,
			PercentComplete: percentComplete,
		}
	}

	return statuses, nil
}

func (d *Deezer) GetConf() (MonitorConfig, error) {
	return MonitorConfig{
		Service:         "deezer",
		CheckInterval:   2 * time.Second,
		MonitorDuration: 5 * time.Minute,
	}, nil
}

func (d *Deezer) Cleanup(track models.Track, id string) error {
	return nil
}
