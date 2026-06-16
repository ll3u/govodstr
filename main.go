package main

import (
	"bufio"
	"context"
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

const appVersion = "v1.0.9"

// global config
var (
	videoDir    = getEnv("VIDEO_DIR", "./vod")
	port        = getEnv("PORT", "8080")
	rateLimitKB = getEnvInt("RATE_LIMIT_KB", 800)
)
var thumbnailMutex sync.Mutex

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>govodstr {{.AppVersion}}</title>
	<link rel="shortcut icon" href="data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20viewBox%3D%220%200%20256%20256%22%3E%3Crect%20width%3D%22256%22%20height%3D%22256%22%20fill%3D%22none%22%2F%3E%3Cpath%20d%3D%22M216,40H40A16,16,0,0,0,24,56V200a16,16,0,0,0,16,16H216a16,16,0,0,0,16-16V56A16,16,0,0,0,216,40ZM184,56h32V72H184ZM72,200H40V184H72ZM72,72H40V56H72Zm48,128H88V184h32Zm0-128H88V56h32Zm48,128H136V184h32Zm0-128H136V56h32Zm48,128H184V184h32v16Z%22%20fill%3D%22%233268b5%22%2F%3E%3C%2Fsvg%3E" >
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
        .artist { font-size: 0.7rem; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; display: flex; justify-content: space-between; align-items: center; width: 100%;}
		.date { font-size: 0.7rem; color: var(--text-muted); padding-left: 5px;font-weight:bold; }

		.breadcrumbs { 
            font-size: 0.8rem; 
            color: var(--text-muted); 
            font-family: monospace; 
            background: #1a1a22; 
            padding: 4px 8px; 
            border-radius: 4px; 
            display: inline-flex; 
            flex-wrap: wrap;
            gap: 4px;
            margin-top: 4px; 
        }
        .breadcrumbs a { color: var(--accent-blue); text-decoration: none; font-weight: 600; }
        .breadcrumbs a:hover { text-decoration: underline; }
        .breadcrumbs span.separator { color: #4b5563; margin: 0 2px; }
        .breadcrumbs span.current-folder { color: var(--text-main); font-weight: 600; }
    </style>
</head>
<body>
    <header>
        <div class="breadcrumbs">
            {{range $index, $bc := .Breadcrumbs}}
                {{if $index}}<span class="separator">/</span>{{end}}
                <a href="/{{$bc.Path}}">{{$bc.Name}}</a>
            {{end}}
        </div>
    </header>
    <div class="playlist-info">
        <a href="{{.BaseURL}}/{{.CurrentDir}}playlist.m3u8"><code>{{.BaseURL}}/{{.CurrentDir}}playlist.m3u8</code></a>
    </div>
    {{if .CurrentDir}}
        <a href="/{{.ParentDir}}" class="btn-back">⬅ back</a>
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
                        <img src="/thumbnails/{{.FullPath}}.jpg" alt="preview">
                    </div>
                    <div class="info">
                        <div class="meta-text">
                            <div class="title" title="{{if .MetaTitle}}{{.MetaTitle}}{{else}}{{.Name}}{{end}}">
                                {{if .MetaTitle}}{{.MetaTitle}}{{else}}{{.Name}}{{end}}
                            </div>
                            <div class="artist">
								{{if .MetaArtist}}
									<span>✨ {{.MetaArtist}}</span>
								{{else}}
									<span></span>
								{{end}}
								{{if .MetaDate}}
									<span class="date">{{.MetaDate}}</span>
								{{end}}
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
	MetaDate   string
}

type FFProbeOutput struct {
	Format struct {
		Tags struct {
			Title  string `json:"title"`
			Artist string `json:"artist"`
			Date   string `json:"creation_time"`
		} `json:"tags"`
	} `json:"format"`
}

type Breadcrumb struct {
	Name string
	Path string
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
		http.Error(w, "directory not found", http.StatusNotFound)
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

	folderCache := loadOrUpdateFolderCache(targetDir, files)

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
			folders = append(folders, RepoItem{Name: name, FullPath: relPath, IsDir: true})
		} else if strings.HasSuffix(strings.ToLower(name), ".mp4") {

			// skip broken or empty video
			if info, err := f.Info(); err == nil && info.Size() < 1024*1024 {
				continue
			}

			escapedURLPath := pathEscapeURI(relPath)
			streamUrl := fmt.Sprintf("%s/stream/%s", hostUrl, escapedURLPath)

			metaTitle := ""
			metaArtist := ""
			metaDate := ""
			if cached, exists := folderCache[name]; exists {
				metaTitle = cached.Title
				metaArtist = cached.Artist
				metaDate = formatDate(cached.Date)
			}

			videos = append(videos, RepoItem{
				Name: strings.TrimSuffix(name, ".mp4"), FullPath: escapedURLPath, IsDir: false,
				MpvIntent: template.URL(streamUrl), MetaTitle: metaTitle, MetaArtist: metaArtist, MetaDate: metaDate,
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

	var breadcrumbs []Breadcrumb
	breadcrumbs = append(breadcrumbs, Breadcrumb{Name: "..", Path: ""})

	if subDir != "" {
		parts := strings.Split(subDir, "/")
		currentPath := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if currentPath == "" {
				currentPath = part
			} else {
				currentPath = currentPath + "/" + part
			}
			breadcrumbs = append(breadcrumbs, Breadcrumb{
				Name: part,
				Path: pathEscapeURI(currentPath) + "/",
			})
		}
	}

	_ = tmpl.Execute(w, map[string]interface{}{
		"Host":        r.Host,
		"BaseURL":     hostUrl,
		"CurrentDir":  currentURLPath,
		"ParentDir":   parentDir,
		"Items":       allItems,
		"AppVersion":  appVersion,
		"Breadcrumbs": breadcrumbs,
	})
}

func generateFolderPlaylist(w http.ResponseWriter, r *http.Request, subDir string) {
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

			// skip broken or empty video
			if info, err := f.Info(); err == nil && info.Size() < 1024*1024 {
				continue
			}

			relPath := f.Name()
			if subDir != "" {
				relPath = subDir + "/" + f.Name()
			}
			escapedPath := pathEscapeURI(relPath)
			folderCache := loadOrUpdateFolderCache(targetDir, files)
			sb.WriteString(fmt.Sprintf("#EXTINF:-1,%s\n", folderCache[f.Name()].Title))
			sb.WriteString(fmt.Sprintf("%s/stream/%s\n", hostUrl, escapedPath))
		}
	}
	_, _ = w.Write([]byte(sb.String()))
}

func handleStreamRoute(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/stream/")
	if relPath == "" || strings.Contains(relPath, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
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
	rawPath := r.URL.EscapedPath()
	escapedName := strings.TrimSuffix(strings.TrimPrefix(rawPath, "/thumbnails/"), ".jpg")

	fileName, err := url.QueryUnescape(escapedName)
	if err != nil || fileName == "" || strings.Contains(fileName, "..") {
		http.Error(w, "invalid", http.StatusBadRequest)
		return
	}

	cacheName := strings.ReplaceAll(url.QueryEscape(fileName), "%2F", "_")
	cachePath := filepath.Join("./thumbnail_cache", cacheName+".jpg")

	if _, err := os.Stat(cachePath); err == nil {
		http.ServeFile(w, r, cachePath)
		return
	}

	thumbnailMutex.Lock()
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		videoPath := filepath.Join(videoDir, fileName)
		// kill ffmpeg when running more then 3 sec
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videoPath, "-ss", "00:00:04", "-vframes", "1", "-threads", "1", "-q:v", "5", cachePath)

		_, ffmpegErr := cmd.CombinedOutput()
		cancel()

		if ffmpegErr != nil {
			fmt.Printf("❌  ffmpeg blocked broken [%s]: %v\n", fileName, ffmpegErr)
		}
	}
	thumbnailMutex.Unlock()

	if _, err := os.Stat(cachePath); err == nil {
		http.ServeFile(w, r, cachePath)
	} else {
		http.Error(w, "thumbnail unavailable", http.StatusInternalServerError)
	}
}

func getVideoMetadata(filePath string) (string, string, string) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags=title,artist,creation_time", "-of", "json", filePath)
	out, err := cmd.Output()
	if err != nil {
		return "", "", ""
	}
	var data FFProbeOutput
	if err := json.Unmarshal(out, &data); err != nil {
		return "", "", ""
	}
	return data.Format.Tags.Title, data.Format.Tags.Artist, data.Format.Tags.Date
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

type FolderCache map[string]struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Date   string `json:"creation_time"`
}

func loadOrUpdateFolderCache(targetDir string, files []os.DirEntry) FolderCache {
	cachePath := filepath.Join(targetDir, ".data.json")
	cache := make(FolderCache)

	if data, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(data, &cache)
	}

	cacheUpdated := false
	scannedThisRun := 0

	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".mp4") {
			continue
		}
		// skip broken or empty files
		if info, err := f.Info(); err == nil && info.Size() < 1024*1024 {
			continue
		}

		name := f.Name()
		if _, exists := cache[name]; !exists {
			if scannedThisRun >= 10 {
				continue
			}

			filePath := filepath.Join(targetDir, name)
			metaTitle, metaArtist, metaDate := getVideoMetadata(filePath)

			if metaTitle == "" {
				metaTitle = strings.TrimSuffix(name, ".mp4")
			}

			cache[name] = struct {
				Title  string `json:"title"`
				Artist string `json:"artist"`
				Date   string `json:"creation_time"`
			}{Title: metaTitle, Artist: metaArtist, Date: metaDate}

			cacheUpdated = true
			scannedThisRun++
		}
	}

	if cacheUpdated {
		if jsonData, err := json.MarshalIndent(cache, "", "  "); err == nil {
			_ = os.WriteFile(cachePath, jsonData, 0644)
		}
	}

	return cache
}

func formatDate(input string) string {
	parsedTime, err := time.Parse(time.RFC3339Nano, input)
	if err != nil {
		return ""
	}
	return parsedTime.Format("2006")
}
