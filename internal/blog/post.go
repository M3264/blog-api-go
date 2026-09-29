package blog

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Post struct {
	Content     json.RawMessage `json:"content,omitempty"`
	ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
	Slug        string          `json:"slug"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Body        string          `json:"body"`
	Category    string          `json:"category"`
	Author      string          `json:"author,omitempty"`
	CoverImage  string          `json:"cover_image,omitempty"`
	Tags        []string        `json:"tags"`
	Featured    bool            `json:"featured"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PublishedAt *time.Time      `json:"published_at,omitempty"`
}

type Input struct {
	ClearSchedule *bool            `json:"clear_schedule"`
	Content       *json.RawMessage `json:"content"`
	ScheduledAt   *time.Time       `json:"scheduled_at"`
	Title         *string          `json:"title"`
	Summary       *string          `json:"summary"`
	Body          *string          `json:"body"`
	Category      *string          `json:"category"`
	Author        *string          `json:"author"`
	CoverImage    *string          `json:"cover_image"`
	Tags          *[]string        `json:"tags"`
	Featured      *bool            `json:"featured"`
	Status        *string          `json:"status"`
}

type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func (in Input) Apply(post *Post, now time.Time) error {
	previous := post.Status
	if in.ClearSchedule != nil && *in.ClearSchedule {
		post.ScheduledAt = nil
	}
	if in.Content != nil {
		_, text, err := RenderContent(*in.Content, "")
		if err != nil {
			return ValidationError{err.Error()}
		}
		post.Content = *in.Content
		post.Body = text
	}
	if in.ScheduledAt != nil {
		post.ScheduledAt = in.ScheduledAt
		post.Status = "draft"
	}
	if in.Title != nil {
		post.Title = strings.TrimSpace(*in.Title)
	}
	if in.Summary != nil {
		post.Summary = strings.TrimSpace(*in.Summary)
	}
	if in.Body != nil && in.Content == nil {
		post.Content = nil
		post.Body = strings.TrimSpace(*in.Body)
	}
	if in.Category != nil {
		post.Category = strings.TrimSpace(*in.Category)
	}
	if in.Author != nil {
		post.Author = strings.TrimSpace(*in.Author)
	}
	if in.CoverImage != nil {
		post.CoverImage = strings.TrimSpace(*in.CoverImage)
	}
	if in.Tags != nil {
		post.Tags = make([]string, len(*in.Tags))
		for i, tag := range *in.Tags {
			post.Tags[i] = strings.TrimSpace(tag)
		}
	}
	if in.Featured != nil {
		post.Featured = *in.Featured
	}
	if in.Status != nil {
		post.Status = strings.TrimSpace(*in.Status)
	}
	if err := post.Validate(); err != nil {
		return ValidationError{Message: err.Error()}
	}
	if post.Status == "published" && previous != "published" {
		t := now.UTC()
		post.PublishedAt = &t
	} else if post.Status == "draft" {
		post.PublishedAt = nil
	}
	if post.Status == "published" {
		post.ScheduledAt = nil
	}
	post.UpdatedAt = now.UTC()
	return nil
}

func (p Post) Validate() error {
	switch {
	case utf8.RuneCountInString(p.Title) < 3 || utf8.RuneCountInString(p.Title) > 160:
		return errors.New("title must be 3 to 160 characters")
	case utf8.RuneCountInString(p.Summary) < 10 || utf8.RuneCountInString(p.Summary) > 300:
		return errors.New("summary must be 10 to 300 characters")
	case utf8.RuneCountInString(p.Body) < 20 || utf8.RuneCountInString(p.Body) > 100000:
		return errors.New("body must be 20 to 100000 characters")
	case utf8.RuneCountInString(p.Category) < 2 || utf8.RuneCountInString(p.Category) > 50:
		return errors.New("category must be 2 to 50 characters")
	case utf8.RuneCountInString(p.Author) > 100:
		return errors.New("author must be at most 100 characters")
	case p.Status != "draft" && p.Status != "published":
		return errors.New("status must be draft or published")
	case len(p.Tags) > 10:
		return errors.New("tags must have at most 10 items")
	}
	if p.CoverImage != "" {
		u, err := url.Parse(p.CoverImage)
		if strings.HasPrefix(p.CoverImage, "/media/") && SafeURL(p.CoverImage, true) {
			u.Scheme = "https"
			u.Host = "local"
		}
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("cover_image must be an absolute HTTP or HTTPS URL")
		}
	}
	seen := make(map[string]bool, len(p.Tags))
	for _, tag := range p.Tags {
		if utf8.RuneCountInString(tag) < 2 || utf8.RuneCountInString(tag) > 30 {
			return errors.New("each tag must be 2 to 30 characters")
		}
		key := strings.ToLower(tag)
		if seen[key] {
			return errors.New("tags must be unique")
		}
		seen[key] = true
	}
	return nil
}

var slugChars = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(title string) string {
	slug := strings.Trim(slugChars.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 80 {
		slug = strings.TrimRight(slug[:80], "-")
	}
	if slug == "" {
		return "post"
	}
	return slug
}

func HasTag(tags []string, wanted string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, wanted) {
			return true
		}
	}
	return false
}
