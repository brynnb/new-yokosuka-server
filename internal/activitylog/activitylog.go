package activitylog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Timestamp        time.Time `json:"timestamp"`
	Type             string    `json:"type"`
	PlayerID         string    `json:"playerId,omitempty"`
	Name             string    `json:"name,omitempty"`
	WorldID          string    `json:"worldId,omitempty"`
	Text             string    `json:"text,omitempty"`
	RemoteIP         string    `json:"remoteIp,omitempty"`
	UserAgent        string    `json:"userAgent,omitempty"`
	SessionSeconds   float64   `json:"sessionSeconds,omitempty"`
	ConnectedClients int       `json:"connectedClients"`
}

type Recorder struct {
	mu   sync.Mutex
	file *os.File
}

func Open(path string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(
		path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o640,
	)
	if err != nil {
		return nil, err
	}
	return &Recorder{file: file}, nil
}

func (r *Recorder) Record(event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.file.Write(payload); err != nil {
		return err
	}
	return r.file.Sync()
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}
