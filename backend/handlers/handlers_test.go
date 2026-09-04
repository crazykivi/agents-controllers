package handlers

import (
	"path/filepath"
	"testing"
)

func TestValidateAgent(t *testing.T) {
	goodDir := t.TempDir()
	tests := []struct {
		name    string
		req     agentReq
		wantErr bool
	}{
		{
			name: "ok minimal",
			req:  agentReq{Name: "alpha", WorkDir: goodDir},
		},
		{
			name: "ok with flags",
			req: agentReq{Name: "a.b-c_1", WorkDir: goodDir,
				Flags: []string{"--no-gitignore", "--check-update", "-v", "--encoding=utf-8"}},
		},
		{name: "bad name spaces", req: agentReq{Name: "bad name", WorkDir: goodDir}, wantErr: true},
		{name: "bad name leading dash", req: agentReq{Name: "-alpha", WorkDir: goodDir}, wantErr: true},
		{name: "relative workdir", req: agentReq{Name: "a", WorkDir: "some/relative"}, wantErr: true},
		{name: "missing workdir", req: agentReq{Name: "a", WorkDir: filepath.Join(goodDir, "nope")}, wantErr: true},
		{name: "flag injection", req: agentReq{Name: "a", WorkDir: goodDir, Flags: []string{"--model; rm -rf /"}}, wantErr: true},
		{name: "flag newline", req: agentReq{Name: "a", WorkDir: goodDir, Flags: []string{"--x\n--y"}}, wantErr: true},
		{name: "bad env key", req: agentReq{Name: "a", WorkDir: goodDir, Env: map[string]string{"lower_key": "v"}}, wantErr: true},
		{name: "env value newline", req: agentReq{Name: "a", WorkDir: goodDir, Env: map[string]string{"K": "v\nw"}}, wantErr: true},
		{name: "model newline", req: agentReq{Name: "a", WorkDir: goodDir, Model: "m\nm"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgent(&tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAgent() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
