// Package routing measures road distances between two points with an
// OSRM-compatible server (https://project-osrm.org): self-hosted OSRM
// (OpenStreetMap roads) in production, the public demo only for trying out.
package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Route is a driving route: its length and its shape on the map ([lat, lng] points).
type Route struct {
	Km   float64
	Path [][2]float64
}

// Router gives the driving route between two points.
type Router interface {
	Route(ctx context.Context, fromLat, fromLng, toLat, toLng float64) (Route, error)
}

// OSRM calls GET {BaseURL}/route/v1/driving/{lng},{lat};{lng},{lat}. Results are
// cached (points rounded to ~1 m), so re-pricing or re-drawing the same order
// costs nothing.
type OSRM struct {
	BaseURL string
	client  *http.Client

	mu    sync.Mutex
	cache map[string]Route
}

const maxCache = 3000

func NewOSRM(baseURL string) *OSRM {
	return &OSRM{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: 6 * time.Second},
		cache:   map[string]Route{},
	}
}

func (o *OSRM) Route(ctx context.Context, fromLat, fromLng, toLat, toLng float64) (Route, error) {
	key := fmt.Sprintf("%.5f,%.5f;%.5f,%.5f", fromLat, fromLng, toLat, toLng)
	o.mu.Lock()
	if r, ok := o.cache[key]; ok {
		o.mu.Unlock()
		return r, nil
	}
	o.mu.Unlock()

	// Full geometry so the line sits on the roads (a few hundred points for a city trip).
	url := fmt.Sprintf("%s/route/v1/driving/%.6f,%.6f;%.6f,%.6f?overview=full&geometries=geojson&alternatives=false",
		o.BaseURL, fromLng, fromLat, toLng, toLat)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Route{}, err
	}
	req.Header.Set("User-Agent", "MedSend/1.0")
	res, err := o.client.Do(req)
	if err != nil {
		return Route{}, err
	}
	defer res.Body.Close()

	var body struct {
		Code   string `json:"code"`
		Routes []struct {
			Distance float64 `json:"distance"` // meters
			Geometry struct {
				Coordinates [][2]float64 `json:"coordinates"` // [lng, lat]
			} `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Route{}, fmt.Errorf("osrm: %w", err)
	}
	if body.Code != "Ok" || len(body.Routes) == 0 {
		return Route{}, errors.New("osrm: rute tidak ditemukan (" + body.Code + ")")
	}
	best := body.Routes[0]
	route := Route{Km: math.Round(best.Distance/100) / 10, Path: make([][2]float64, 0, len(best.Geometry.Coordinates))}
	for _, c := range best.Geometry.Coordinates {
		route.Path = append(route.Path, [2]float64{c[1], c[0]})
	}

	o.mu.Lock()
	if len(o.cache) >= maxCache {
		o.cache = map[string]Route{}
	}
	o.cache[key] = route
	o.mu.Unlock()
	return route, nil
}
