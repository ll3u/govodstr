package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// global config
var (
	videoDir    = getEnv("VIDEO_DIR", "./vod")
	port        = getEnv("PORT", "8080")
	rateLimitKB = getEnvInt("RATE_LIMIT_KB", 800)
)
var thumbnailMutex sync.Mutex

const htmlTemplate = `
<!DOCTYPE html>
<html lang="de">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Mini Streamer</title>
    <style>
        :root {
            --bg-color: #060608;
            --card-bg: #121216;
            --border-color: #1e1e24;
            --text-main: #f3f4f6;
            --text-muted: #9ca3af;
            --accent-blue: #2563eb;
            --accent-blue-hover: #1d4ed8;
            --accent-amber: #d97706;
        }

        * { box-sizing: border-box; margin: 0; padding: 0; }

        body { 
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; 
            background-color: var(--bg-color); 
            color: var(--text-main); 
            padding: 12px;
            line-height: 1.35;
        }

        header { margin-bottom: 12px; }
        h1 { font-size: 1.35rem; font-weight: 700; color: #fff; }
        .current-dir { font-size: 0.75rem; color: var(--text-muted); font-family: monospace; background: #1a1a22; padding: 2px 5px; border-radius: 4px; display: inline-block; margin-top: 2px; }
        
        .playlist-info { 
            background: #121216; 
            padding: 10px; border-radius: 8px; margin-bottom: 15px; border: 1px solid var(--border-color);
        }
        .playlist-info strong { display: block; font-size: 0.75rem; color: var(--text-muted); margin-bottom: 3px; }
        .playlist-info code { display: block; color: #34d399; font-size: 0.8rem; font-family: monospace; word-break: break-all; background: #000; padding: 5px 8px; border-radius: 5px; }
        
        .btn-back { display: inline-flex; align-items: center; background: #1e1e24; color: var(--text-main); padding: 6px 12px; border-radius: 5px; text-decoration: none; font-size: 0.8rem; margin-bottom: 12px; border: 1px solid var(--border-color); }
        
        .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
        
        .card { 
            background: var(--card-bg); border-radius: 8px; overflow: hidden; border: 1px solid var(--border-color); 
            display: flex; flex-direction: column; transition: transform 0.2s, box-shadow 0.2s, border-color 0.2s;
            text-decoration: none; color: inherit;
        }
        
        .card.folder-card { 
            flex-direction: row; align-items: center; padding: 8px 10px; background: #16161c;
        }
        .card.folder-card:hover { border-color: var(--accent-amber); transform: translateY(-1px); }
        .card.folder-card .folder-icon { font-size: 1.3rem; margin-right: 8px; color: var(--accent-amber); display: flex; align-items: center; }
        .card.folder-card .title { font-size: 0.8rem; font-weight: 600; color: #fff; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; height: auto; display: block; }
        
        .card.video-card:hover { border-color: var(--accent-blue); transform: translateY(-2px); }
        
        .thumbnail-wrapper { position: relative; width: 100%; aspect-ratio: 2.4 / 1; background: #000; overflow: hidden; }
        .card img { width: 100%; height: 100%; object-fit: cover; object-position: center; }
        
        .info { padding: 8px; flex-grow: 1; display: flex; flex-direction: column; justify-content: center; gap: 2px; }
        .meta-text { display: flex; flex-direction: column; gap: 1px; }
        .title { font-size: 0.85rem; font-weight: 600; color: #fff; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; height: 2.3rem; line-height: 1.3; }
        .artist { font-size: 0.7rem; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    </style>
</head>
<body>
    <header>
        <div class="current-dir">/{{if .CurrentDir}}{{.CurrentDir}}{{end}}</div>
    </header>
    <div class="playlist-info">
        <a href="{{.BaseURL}}/{{.CurrentDir}}playlist.m3u8"><code>{{.BaseURL}}/{{.CurrentDir}}playlist.m3u8</code></a>
    </div>
    {{if .CurrentDir}}
        <a href="/{{.ParentDir}}" class="btn-back">⬅ Zurück</a>
    {{end}}
    <div class="grid">
        {{range .Items}}
            {{if .IsDir}}
                <a href="/{{.FullPath}}/" class="card folder-card">
                    <div class="folder-icon">📁</div>
                    <div class="title" title="{{.Name}}">{{.Name}}</div>
                </a>
            {{else}}
                <a href="{{.MpvIntent}}" class="card video-card" target="_blank">
                    <div class="thumbnail-wrapper">
                        <img src="/thumbnails/{{.FullPath}}.jpg" alt="Vorschau">
                    </div>
                                       <div class="info">
                        <div class="meta-text">
                            <div class="title" title="{{.Name}}">
                                {{.Name}}
                            </div>
                        </div>
                    </div>
                </a>
            {{end}}
        {{end}}
    </div>
</body>
</html>
`

type RepoItem struct {
	Name       string
	FullPath   string
	IsDir      bool
	MpvIntent  template.URL
	MetaTitle  string
	MetaArtist string
}

type FFProbeOutput struct {
	Format struct {
		Tags struct {
			Title  string `json:"title"`
			Artist string `json:"artist"`
		} `json:"tags"`
	} `json:"format"`
}

type ThrottledReadSeekCloser struct {
	io.ReadSeeker
	bufferedReader *bufio.Reader
	io.Closer
	limitKB   int
	bytesRead int64
	burstSize int64
}

func (tr *ThrottledReadSeekCloser) Read(p []byte) (n int, err error) {
	start := time.Now()
	n, err = tr.bufferedReader.Read(p)
	if n <= 0 {
		return n, err
	}
	tr.bytesRead += int64(n)
	if tr.bytesRead > tr.burstSize && tr.limitKB > 0 {
		desiredDuration := time.Duration(n) * time.Second / time.Duration(tr.limitKB*1024)
		actualDuration := time.Since(start)
		if desiredDuration > actualDuration {
			time.Sleep(desiredDuration - actualDuration)
		}
	}
	return n, err
}

func (tr *ThrottledReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	tr.bytesRead = 0
	pos, err := tr.ReadSeeker.Seek(offset, whence)
	if err == nil {
		tr.bufferedReader.Reset(tr.ReadSeeker)
	}
	return pos, err
}

func main() {
	_ = os.MkdirAll("./thumbnail_cache", os.ModePerm)
	http.HandleFunc("/stream/", handleStreamRoute)
	http.HandleFunc("/thumbnails/", handleThumbnail)
	http.HandleFunc("/", handleCatchAll)
	fmt.Printf("govodstr running on port %s...\n", port)
	fmt.Printf("vod dir: %s | limit: %d KB/s\n", videoDir, rateLimitKB)
	_ = http.ListenAndServe(":"+port, nil)
}

func handleCatchAll(w http.ResponseWriter, r *http.Request) {
	fullPath := r.URL.Path
	if strings.HasSuffix(fullPath, "playlist.m3u8") {
		subDir := strings.TrimSuffix(fullPath, "playlist.m3u8")
		generateFolderPlaylist(w, r, strings.Trim(subDir, "/"))
		return
	}
	renderFolderIndex(w, r, strings.Trim(fullPath, "/"))
}

func renderFolderIndex(w http.ResponseWriter, r *http.Request, subDir string) {
	if strings.Contains(subDir, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	targetDir := filepath.Join(videoDir, subDir)
	files, err := os.ReadDir(targetDir)
	if err != nil {
		http.Error(w, "dir not found", http.StatusNotFound)
		return
	}
	parentDir := ""
	if subDir != "" {
		parentDir = filepath.ToSlash(filepath.Dir(subDir))
		if parentDir == "." {
			parentDir = ""
		}
	}

	protocol := "http://"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		protocol = "https://"
	}
	hostUrl := protocol + r.Host

	var folders []RepoItem
	var videos []RepoItem
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		relPath := name
		if subDir != "" {
			relPath = subDir + "/" + name
		}

		if f.IsDir() {
			folders = append(folders, RepoItem{
				Name:     name,
				FullPath: relPath,
				IsDir:    true,
			})
		} else if strings.HasSuffix(strings.ToLower(name), ".mp4") {
			escapedURLPath := pathEscapeURI(relPath)
			streamUrl := fmt.Sprintf("http://%s/stream/%s", r.Host, escapedURLPath)

			videos = append(videos, RepoItem{
				Name:      strings.TrimSuffix(name, ".mp4"),
				FullPath:  escapedURLPath,
				IsDir:     false,
				MpvIntent: template.URL(streamUrl),
			})
		}
	}
	var allItems []RepoItem
	allItems = append(allItems, folders...)
	allItems = append(allItems, videos...)

	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	currentURLPath := subDir
	if currentURLPath != "" {
		currentURLPath = currentURLPath + "/"
	}

	_ = tmpl.Execute(w, map[string]interface{}{
		"Host":       r.Host,
		"BaseURL":    hostUrl,
		"CurrentDir": currentURLPath,
		"ParentDir":  parentDir,
		"Items":      allItems,
	})
}

func generateFolderPlaylist(w http.ResponseWriter, r *http.Request, subDir string) {
	if strings.Contains(subDir, "..") {
		http.Error(w, "Ungültiger Pfad", http.StatusBadRequest)
		return
	}
	targetDir := filepath.Join(videoDir, subDir)
	files, err := os.ReadDir(targetDir)
	if err != nil {
		http.Error(w, "dir not found", http.StatusNotFound)
		return
	}

	protocol := "http://"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		protocol = "https://"
	}
	hostUrl := protocol + r.Host

	w.Header().Set("Content-Type", "application/x-mpegURL")
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".mp4") {
			relPath := f.Name()
			if subDir != "" {
				relPath = subDir + "/" + f.Name()
			}
			escapedPath := pathEscapeURI(relPath)
			sb.WriteString(fmt.Sprintf("#EXTINF:-1,%s\n", strings.TrimSuffix(f.Name(), ".mp4")))
			sb.WriteString(fmt.Sprintf("%s/stream/%s\n", hostUrl, escapedPath))
		}
	}
	_, _ = w.Write([]byte(sb.String()))
}

func handleStreamRoute(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/stream/")
	if relPath == "" || strings.Contains(relPath, "..") {
		http.Error(w, "Pfad ungültig", http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(videoDir, relPath)
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	largeReader := bufio.NewReaderSize(file, 64*1024)
	throttledFile := &ThrottledReadSeekCloser{
		ReadSeeker:     file,
		bufferedReader: largeReader,
		Closer:         file,
		limitKB:        rateLimitKB,
		burstSize:      50 * 1024 * 1024,
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", fileInfo.Name()))
	http.ServeContent(w, r, fileInfo.Name(), fileInfo.ModTime(), throttledFile)
}

func handleThumbnail(w http.ResponseWriter, r *http.Request) {
	escapedName := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/thumbnails/"), ".jpg")
	fileName, err := url.QueryUnescape(escapedName)
	if err != nil || fileName == "" || strings.Contains(fileName, "..") {
		http.Error(w, "Ungültig", http.StatusBadRequest)
		return
	}
	cachePath := filepath.Join("./thumbnail_cache", strings.ReplaceAll(url.QueryEscape(fileName), "%2F", "_")+".jpg")

	if _, err := os.Stat(cachePath); err == nil {
		http.ServeFile(w, r, cachePath)
		return
	}
	thumbnailMutex.Lock()

	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		cmd := exec.Command("ffmpeg", "-y", "-ss", "00:01:00", "-i", filepath.Join(videoDir, fileName), "-vframes", "1", "-threads", "1", "-q:v", "5", cachePath)
		_ = cmd.Run()
	}
	thumbnailMutex.Unlock()

	if _, err := os.Stat(cachePath); err == nil {
		http.ServeFile(w, r, cachePath)
	} else {
		http.Error(w, "Thumbnail-Generierung fehlgeschlagen", http.StatusInternalServerError)
	}
}

func getVideoMetadata(filePath string) (string, string) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags=title,artist", "-of", "json", filePath)
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	var data FFProbeOutput
	if err := json.Unmarshal(out, &data); err != nil {
		return "", ""
	}
	return data.Format.Tags.Title, data.Format.Tags.Artist
}

func pathEscapeURI(rawPath string) string {
	parts := strings.Split(rawPath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}
