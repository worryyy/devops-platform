package app

import "embed"

//go:embed frontend/dist/*
var frontendFS embed.FS
