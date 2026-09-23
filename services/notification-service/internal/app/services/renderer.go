package services

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFiles embed.FS

var ErrUnknownTemplate = errors.New("unknown template")

type Rendered struct {
	Subject string
	Body    string
}

type Renderer struct {
	templates map[string]*template.Template
}

func Templates() fs.FS {
	files, _ := fs.Sub(templateFiles, "templates")
	return files
}

func NewRenderer(files fs.FS) (*Renderer, error) {
	names, _ := fs.Glob(files, "*.tmpl")
	templates := make(map[string]*template.Template, len(names))
	for _, name := range names {
		parsed, err := template.New(name).Option("missingkey=error").ParseFS(files, name)
		if err != nil {
			return nil, err
		}
		templates[strings.TrimSuffix(path.Base(name), ".tmpl")] = parsed
	}
	return &Renderer{templates: templates}, nil
}

func (r *Renderer) Render(kind string, data map[string]string) (Rendered, error) {
	parsed, ok := r.templates[kind]
	if !ok {
		return Rendered{}, fmt.Errorf("%w: %s", ErrUnknownTemplate, kind)
	}
	sections := make(map[string]string, 2)
	for _, name := range []string{"subject", "body"} {
		var buf strings.Builder
		if err := parsed.ExecuteTemplate(&buf, name, data); err != nil {
			return Rendered{}, err
		}
		sections[name] = strings.TrimSpace(buf.String())
	}
	return Rendered{Subject: sections["subject"], Body: sections["body"]}, nil
}

func FormatBRL(amount float64) string {
	cents := int64(math.Round(amount * 100))
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	whole := strconv.FormatInt(cents/100, 10)
	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}
	return fmt.Sprintf("%sR$ %s,%02d", sign, grouped.String(), cents%100)
}
