package httpapi

import (
	"fmt"
	"github.com/M3264/blog-api-go/internal/blog"
	"github.com/M3264/blog-api-go/internal/community"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
)

func validEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, e := mail.ParseAddress(s)
	if e != nil || a.Address != s || len(s) > 254 {
		return "", fmt.Errorf("invalid email")
	}
	return s, nil
}
func safeImage(s string) bool { return blog.SafeURL(s, true) }
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	u, e := a.current(r)
	if e != nil || u.Role != "admin" {
		writeError(w, 403, "admin required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if e = r.ParseMultipartForm(10 << 20); e != nil {
		writeError(w, 400, "upload must be under 10 MB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, e := r.FormFile("image")
	if e != nil {
		writeError(w, 400, "choose a JPEG, PNG or WebP image")
		return
	}
	defer file.Close()
	cfg, format, e := image.DecodeConfig(file)
	if e != nil || (format != "jpeg" && format != "png" && format != "webp") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 12000 || cfg.Height > 12000 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		writeError(w, 400, "invalid image or dimensions exceed 40 megapixels")
		return
	}
	file.Seek(0, 0)
	src, _, e := image.Decode(file)
	if e != nil {
		writeError(w, 400, "invalid image")
		return
	}
	width, height := cfg.Width, cfg.Height
	if width > 2400 {
		height = height * 2400 / width
		width = 2400
	}
	if height > 2400 {
		width = width * 2400 / height
		height = 2400
	}
	width = max(1, width)
	height = max(1, height)
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	dir := a.community.Config.MediaDir
	if e = os.MkdirAll(dir, 0750); e != nil {
		a.internal(w, e)
		return
	}
	id := community.Random()
	name := id + ".jpg"
	out, e := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if e != nil {
		a.internal(w, e)
		return
	}
	e = jpeg.Encode(out, dst, &jpeg.Options{Quality: 88})
	closeErr := out.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		_, e = a.community.DB.ExecContext(r.Context(), `INSERT INTO media(id,user_id,filename,width,height) VALUES($1,$2,$3,$4,$5)`, id, u.ID, name, width, height)
	}
	if e != nil {
		os.Remove(filepath.Join(dir, name))
		a.internal(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"url": "/media/" + name, "width": width, "height": height})
}
func (a *API) media(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if filepath.Base(name) != name || strings.Contains(name, "..") || !strings.HasSuffix(name, ".jpg") {
		http.NotFound(w, r)
		return
	}
	var exists bool
	e := a.community.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM media WHERE filename=$1)`, name).Scan(&exists)
	if e != nil || !exists {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, filepath.Join(a.community.Config.MediaDir, name))
}
