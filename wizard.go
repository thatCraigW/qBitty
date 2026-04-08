package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// wizardEnvEnabled is true when QBITTY_WIZARD or WIZARD is set to a truthy value (inputs: none; output: whether interactive setup should run when qB config is incomplete).
func wizardEnvEnabled() bool {
	for _, k := range []string{"QBITTY_WIZARD", "WIZARD"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
}

// runFirstLaunchWizard prompts for missing qBittorrent fields, optionally Sonarr/Radarr, writes config.json, and returns merged config from disk+env (inputs: partial config from merge; output: full config or error).
func runFirstLaunchWizard(base *Config) (*Config, error) {
	if base == nil {
		base = &Config{}
	}
	cfg := *base

	fmt.Println()
	fmt.Println("qbitty — first-time setup")
	fmt.Println("-------------------------")
	fmt.Println()

	r := bufio.NewReader(os.Stdin)

	if strings.TrimSpace(cfg.URL) == "" {
		s, err := promptNonEmptyLine(r, "qBittorrent Web UI URL (e.g. http://127.0.0.1:8080): ")
		if err != nil {
			return nil, err
		}
		cfg.URL = s
	}
	if strings.TrimSpace(cfg.Username) == "" {
		s, err := promptNonEmptyLine(r, "qBittorrent username: ")
		if err != nil {
			return nil, err
		}
		cfg.Username = s
	}
	if strings.TrimSpace(cfg.Password) == "" {
		pw, err := promptPasswordOnce("qBittorrent password: ")
		if err != nil {
			return nil, err
		}
		cfg.Password = pw
	}

	useSonarr, err := promptYesNo(r, "Configure Sonarr integration? [y/N]: ", false)
	if err != nil {
		return nil, err
	}
	if useSonarr {
		u, err := promptNonEmptyLine(r, "  Sonarr base URL (e.g. http://127.0.0.1:8989): ")
		if err != nil {
			return nil, err
		}
		k, err := promptNonEmptyLine(r, "  Sonarr API key (Settings → General → Security): ")
		if err != nil {
			return nil, err
		}
		cfg.SonarrURL = u
		cfg.SonarrAPIKey = k
	} else {
		cfg.SonarrURL = ""
		cfg.SonarrAPIKey = ""
	}

	useRadarr, err := promptYesNo(r, "Configure Radarr integration? [y/N]: ", false)
	if err != nil {
		return nil, err
	}
	if useRadarr {
		u, err := promptNonEmptyLine(r, "  Radarr base URL (e.g. http://127.0.0.1:7878): ")
		if err != nil {
			return nil, err
		}
		k, err := promptNonEmptyLine(r, "  Radarr API key (Settings → General → Security): ")
		if err != nil {
			return nil, err
		}
		cfg.RadarrURL = u
		cfg.RadarrAPIKey = k
	} else {
		cfg.RadarrURL = ""
		cfg.RadarrAPIKey = ""
	}

	path, err := PrimaryConfigWritePath()
	if err != nil {
		return nil, err
	}
	if err := writeConfigFile(path, &cfg); err != nil {
		return nil, err
	}
	fmt.Println()
	fmt.Printf("Saved configuration to %s\n", path)
	fmt.Println()

	return mergeConfigFromFileAndEnv()
}

// promptNonEmptyLine reads a non-empty trimmed line (inputs: reader and label; output: string or read error).
func promptNonEmptyLine(r *bufio.Reader, label string) (string, error) {
	for {
		fmt.Print(label)
		s, err := readLineTrimmed(r)
		if err != nil {
			return "", err
		}
		if s != "" {
			return s, nil
		}
		fmt.Println("  (required)")
	}
}

// readLineTrimmed reads one line and trims spaces (input: reader; output: trimmed string or error).
func readLineTrimmed(r *bufio.Reader) (string, error) {
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// promptPasswordOnce reads a password without echo (input: label; output: password or error).
func promptPasswordOnce(label string) (string, error) {
	fmt.Print(label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	fmt.Println()
	return string(b), nil
}

// promptYesNo reads y/n; defaultNo when empty (inputs: reader, prompt, default when empty is no; output: yes/no).
func promptYesNo(r *bufio.Reader, label string, defaultYes bool) (bool, error) {
	for {
		fmt.Print(label)
		s, err := readLineTrimmed(r)
		if err != nil {
			return false, err
		}
		if s == "" {
			return defaultYes, nil
		}
		low := strings.ToLower(s)
		if low == "y" || low == "yes" {
			return true, nil
		}
		if low == "n" || low == "no" {
			return false, nil
		}
		fmt.Println("  Please answer y or n.")
	}
}
