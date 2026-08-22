package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

type projectConfig struct {
	Agents struct {
		Enabled             bool `toml:"enabled"`
		IndependentReviewer struct {
			Description string `toml:"description"`
			ConfigFile  string `toml:"config_file"`
		} `toml:"independent_reviewer"`
	} `toml:"agents"`
	Skills struct {
		Config []skillConfig `toml:"config"`
	} `toml:"skills"`
}

type skillConfig struct {
	Path    string `toml:"path"`
	Enabled bool   `toml:"enabled"`
}

type agentDefinition struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	DeveloperInstructions string `toml:"developer_instructions"`
	SandboxMode           string `toml:"sandbox_mode"`
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func main() {
	if err := validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Agent and skill definition checks passed.")
}

func validate() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	configPath := filepath.Join(root, ".codex", "config.toml")

	var config projectConfig
	if err := decodeTOML(configPath, &config); err != nil {
		return err
	}
	if !config.Agents.Enabled {
		return fmt.Errorf("%s: agents.enabled must be true", configPath)
	}
	if strings.TrimSpace(config.Agents.IndependentReviewer.Description) == "" {
		return fmt.Errorf("%s: independent_reviewer description is required", configPath)
	}

	agentPath := filepath.Clean(filepath.Join(filepath.Dir(configPath), filepath.FromSlash(config.Agents.IndependentReviewer.ConfigFile)))
	var agent agentDefinition
	if err := decodeTOML(agentPath, &agent); err != nil {
		return err
	}
	if agent.Name != "independent_reviewer" {
		return fmt.Errorf("%s: agent name must be independent_reviewer", agentPath)
	}
	if strings.TrimSpace(agent.Description) == "" || strings.TrimSpace(agent.DeveloperInstructions) == "" {
		return fmt.Errorf("%s: description and developer_instructions are required", agentPath)
	}
	if agent.SandboxMode != "read-only" {
		return fmt.Errorf("%s: independent reviewer must use read-only sandbox", agentPath)
	}

	registered := make(map[string]bool)
	for _, entry := range config.Skills.Config {
		path := filepath.Clean(filepath.Join(filepath.Dir(configPath), filepath.FromSlash(entry.Path)))
		registered[path] = entry.Enabled
	}

	agentsPath := filepath.Join(root, "AGENTS.md")
	agentsIndex, err := os.ReadFile(agentsPath)
	if err != nil {
		return err
	}
	indexText := string(agentsIndex)
	expectedSkills, err := discoverSkills(filepath.Join(root, ".codex", "skills"))
	if err != nil {
		return err
	}
	for _, name := range expectedSkills {
		skillPath := filepath.Join(root, ".codex", "skills", name, "SKILL.md")
		if !registered[skillPath] {
			return fmt.Errorf("%s is not enabled in %s", skillPath, configPath)
		}
		frontmatter, err := readSkillFrontmatter(skillPath)
		if err != nil {
			return err
		}
		if frontmatter.Name != name {
			return fmt.Errorf("%s: skill name %q must match folder %q", skillPath, frontmatter.Name, name)
		}
		if strings.TrimSpace(frontmatter.Description) == "" {
			return fmt.Errorf("%s: skill description is required", skillPath)
		}
		indexPath := filepath.ToSlash(filepath.Join(".codex", "skills", name, "SKILL.md"))
		if !containsMarkdownLink(indexText, indexPath) {
			return fmt.Errorf("AGENTS.md does not index %s", indexPath)
		}
	}

	registeredNames := make([]string, 0, len(registered))
	for path, enabled := range registered {
		if enabled {
			registeredNames = append(registeredNames, path)
		}
	}
	if len(registeredNames) != len(expectedSkills) {
		sort.Strings(registeredNames)
		return fmt.Errorf("enabled project skills differ from expected set: %v", registeredNames)
	}

	required := []string{
		filepath.Join(root, ".codex", "instructions", "development-cycle.md"),
		filepath.Join(root, ".codex", "instructions", "project-conventions.md"),
		filepath.Join(root, ".codex", "instructions", "independent-review.md"),
	}
	for _, path := range required {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return fmt.Errorf("required instruction file is missing: %s", path)
		}
	}
	if err := validateLocalLinks(agentsPath, indexText); err != nil {
		return err
	}
	return nil
}

func discoverSkills(skillsDir string) ([]string, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if info, err := os.Stat(skillPath); err != nil || info.IsDir() {
			return nil, fmt.Errorf("skill directory must contain SKILL.md: %s", skillPath)
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no project skills found in %s", skillsDir)
	}
	return names, nil
}

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func containsMarkdownLink(markdown, destination string) bool {
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(markdown, -1) {
		if filepath.ToSlash(match[1]) == destination {
			return true
		}
	}
	return false
}

func validateLocalLinks(markdownPath, markdown string) error {
	baseDir := filepath.Dir(markdownPath)
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(markdown, -1) {
		destination := strings.TrimSpace(match[1])
		if strings.Contains(destination, "://") || strings.HasPrefix(destination, "#") {
			continue
		}
		destination = strings.SplitN(destination, "#", 2)[0]
		linkedPath := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(destination)))
		if _, err := os.Stat(linkedPath); err != nil {
			return fmt.Errorf("%s: broken local link %q: %w", markdownPath, match[1], err)
		}
	}
	return nil
}

func decodeTOML(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(content, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func readSkillFrontmatter(path string) (*skillFrontmatter, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("%s: YAML frontmatter is required", path)
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("%s: YAML frontmatter is not closed", path)
	}
	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &frontmatter); err != nil {
		return nil, fmt.Errorf("parse %s frontmatter: %w", path, err)
	}
	return &frontmatter, nil
}
