package llm

import (
        "path/filepath"
        "sync"
        "time"
)

type fileRecord struct {
        path      string
        readTime  time.Time
        writeTime time.Time
}

var (
        fileRecords   = make(map[string]fileRecord)
        fileRecordsMu sync.RWMutex
)

func normalisePath(path string) string {
        if path == "" {
                return ""
        }
        cleaned := filepath.Clean(path)
        if abs, err := filepath.Abs(cleaned); err == nil {
                return abs
        }
        return cleaned
}

func recordFileRead(path string) {
        norm := normalisePath(path)
        fileRecordsMu.Lock()
        defer fileRecordsMu.Unlock()
        rec := fileRecords[norm]
        rec.path = norm
        rec.readTime = time.Now()
        fileRecords[norm] = rec
}

func getLastReadTime(path string) time.Time {
        norm := normalisePath(path)
        fileRecordsMu.RLock()
        defer fileRecordsMu.RUnlock()
        return fileRecords[norm].readTime
}

func recordFileWrite(path string) {
        norm := normalisePath(path)
        fileRecordsMu.Lock()
        defer fileRecordsMu.Unlock()
        rec := fileRecords[norm]
        rec.path = norm
        rec.writeTime = time.Now()
        fileRecords[norm] = rec
}
