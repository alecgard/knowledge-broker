package cluster

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"io"
)

//go:embed viz_template.html
var vizTemplateHTML string

// VizPoint represents a single point in the cluster visualization.
type VizPoint struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Cluster int     `json:"cluster"`
	Topic   string  `json:"topic"`
	Source  string  `json:"source"`
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	ID      string  `json:"id"`
}

// GenerateVizHTML executes the embedded Plotly template and writes the
// self-contained HTML to w.
func GenerateVizHTML(points []VizPoint, w io.Writer) error {
	data, err := json.Marshal(points)
	if err != nil {
		return err
	}

	tmpl, err := template.New("viz").Parse(vizTemplateHTML)
	if err != nil {
		return err
	}

	return tmpl.Execute(w, struct {
		PointsJSON template.JS
	}{
		PointsJSON: template.JS(data),
	})
}
