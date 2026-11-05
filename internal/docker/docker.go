// Package docker получает состояние контейнеров через docker inspect.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"accel-menu/internal/execx"
)

// State — состояние контейнера.
type State struct {
	Name   string
	Status string
	Health string
}

// Running сообщает, запущен ли контейнер.
func (s State) Running() bool { return strings.EqualFold(s.Status, "running") }

// Healthy сообщает, healthy ли контейнер (если healthcheck есть).
func (s State) Healthy() bool { return strings.EqualFold(s.Health, "healthy") }

// Client — клиент docker.
type Client struct {
	run *execx.Runner
}

// New создаёт клиент.
func New(run *execx.Runner) *Client { return &Client{run: run} }

// Inspect возвращает состояние контейнера. Ошибка, если контейнера нет.
func (c *Client) Inspect(ctx context.Context, name string) (State, error) {
	out, err := c.run.RunSudo(ctx, "docker", "inspect", "--format", "{{json .State}}", name)
	if err != nil {
		return State{}, err
	}
	return ParseState(name, out)
}

// ParseState разбирает JSON вида {"Status":"running","Health":{"Status":"healthy"}}.
func ParseState(name, raw string) (State, error) {
	var st struct {
		Status string `json:"Status"`
		Health *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &st); err != nil {
		return State{}, fmt.Errorf("docker inspect %s: %w", name, err)
	}
	res := State{Name: name, Status: st.Status}
	if st.Health != nil {
		res.Health = st.Health.Status
	}
	return res, nil
}

// Display возвращает человекочитаемый статус: running, healthy, ...
func (s State) Display() string {
	if s.Health != "" {
		return s.Status + " / " + s.Health
	}
	return s.Status
}
