package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed prompts/setup-helper.md
var setupHelperInstructions string

func loadChatGuide() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return readChatGuide(executable)
}

func readChatGuide(executable string) (string, error) {
	directory := filepath.Dir(executable)
	paths := []string{filepath.Join(directory, "llms.txt"), "../docs/llms.txt", "docs/llms.txt"}
	if filepath.Base(directory) == "MacOS" && filepath.Base(filepath.Dir(directory)) == "Contents" {
		paths = []string{filepath.Join(directory, "..", "Resources", "llms.txt")}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("The offline help reference is missing. Reinstall the app, or choose Just chat")
}

func (service *DashboardService) SetChatMode(mode string) (ChatState, error) {
	if mode != "help" && mode != "bare" {
		return ChatState{}, fmt.Errorf("Choose Help or Just chat")
	}
	chat := service.conversation
	chat.mu.Lock()
	defer chat.mu.Unlock()
	if chat.state.Streaming || len(chat.state.Messages) > 0 {
		return ChatState{}, fmt.Errorf("Choose New chat before changing its purpose")
	}
	prompt := ""
	if mode == "help" {
		guide, err := chat.guide()
		if err != nil {
			return ChatState{}, err
		}
		prompt = strings.TrimSpace(setupHelperInstructions) + "\n\n--- SETUP REFERENCE ---\n" + guide + "\n--- END REFERENCE ---"
	}
	chat.system = prompt
	chat.state.Mode = mode
	return chat.state, nil
}
