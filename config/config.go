package config

import (
	"log"

	"gopkg.in/yaml.v2"

	"fmt"
	"os"
	"path/filepath"
	"soft-serve/git"

	"github.com/gliderlabs/ssh"
	gg "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Config struct {
	Name         string `yaml:"name"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	AnonReadOnly bool   `yaml:"anon-access"`
	AllowNoKeys  bool   `yaml:"allow-no-keys"`
	Users        []User `yaml:"users"`
	Repos        []Repo `yaml:"repos"`
	Source       *git.RepoSource
}

type User struct {
	Name        string   `yaml:"name"`
	Admin       bool     `yaml:"admin"`
	PublicKey   string   `yaml:"pk"`
	CollabRepos []string `yaml:"collab_repos"`
}

type Repo struct {
	Name string `yaml:"name"`
	Repo string `yaml:"repo"`
	Note string `yaml:"note"`
}

func NewConfig(host string, port int, anon bool, pk string, rs *git.RepoSource) (*Config, error) {
	cfg := &Config{}
	cfg.Host = host
	cfg.Port = port
	cfg.AnonReadOnly = anon
	cfg.Source = rs

	var yamlUsers string
	var h string
	if host == "" {
		h = "localhost"
	} else {
		h = host
	}
	yamlConfig := fmt.Sprintf(defaultConfig, h, port, anon)
	if pk != "" {
		yamlUsers = fmt.Sprintf(hasKeyUserConfig, pk)
	} else {
		yamlUsers = defaultUserConfig
	}
	yaml := fmt.Sprintf("%s%s%s", yamlConfig, yamlUsers, exampleUserConfig)
	err := cfg.createDefaultConfigRepo(yaml)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (cfg *Config) Pushed(repo string, pk ssh.PublicKey) {
	err := cfg.Reload()
	if err != nil {
		log.Printf("error reloading after push: %s", err)
	}
}

func (cfg *Config) Reload() error {
	err := cfg.Source.LoadRepos()
	if err != nil {
		return err
	}
	cr, err := cfg.Source.GetRepo("config")
	if err != nil {
		return err
	}
	cs, err := cr.LatestFile("config.yaml")
	if err != nil {
		return err
	}
	err = yaml.Unmarshal([]byte(cs), cfg)
	if err != nil {
		return fmt.Errorf("bad yaml in config.yaml: %s", err)
	}
	return nil
}

func createFile(path string, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	if err != nil {
		return err
	}
	return f.Sync()
}

func (cfg *Config) createDefaultConfigRepo(yaml string) error {
	cn := "config"
	rs := cfg.Source
	err := rs.LoadRepos()
	if err != nil {
		return err
	}
	_, err = rs.GetRepo(cn)
	if err == git.ErrMissingRepo {
		cr, err := rs.InitRepo(cn, false)
		if err != nil {
			return err
		}

		rp := filepath.Join(rs.Path, cn, "README.md")
		err = createFile(rp, defaultReadme)
		if err != nil {
			return err
		}
		cp := filepath.Join(rs.Path, cn, "config.yaml")
		err = createFile(cp, yaml)
		if err != nil {
			return err
		}
		wt, err := cr.Repository.Worktree()
		if err != nil {
			return err
		}
		_, err = wt.Add("README.md")
		if err != nil {
			return err
		}
		_, err = wt.Add("config.yaml")
		if err != nil {
			return err
		}
		_, err = wt.Commit("Default init", &gg.CommitOptions{
			All: true,
			Author: &object.Signature{
				Name:  "Soft Serve Server",
				Email: "vt100@charm.sh",
			},
		})
		if err != nil {
			return err
		}
		err = rs.LoadRepos()
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}