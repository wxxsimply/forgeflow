package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"forgeflow/internal/eval"
)

type lockFile struct {
	Dataset string      `json:"dataset"`
	Version string      `json:"version"`
	Cases   []lockEntry `json:"cases"`
}

type lockEntry struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
	Ref    string `json:"ref"`
}

type caseDescriptor struct {
	ID               string        `json:"id"`
	Category         eval.Category `json:"category"`
	Task             string        `json:"task"`
	ExpectedDecision eval.Decision `json:"expectedDecision"`
}

func main() {
	fixtureRepository := flag.String("fixture-repository", "", "new local fixture repository directory")
	templateDirectory := flag.String("template", "evals/fixtures/public", "public fixture template directory")
	datasetPath := flag.String("dataset", "internal/eval/datasets/software_v1.json", "dataset file to update")
	lockPath := flag.String("lock-output", "evals/software-v1-fixtures.lock.json", "fixture lock file to write")
	updateDataset := flag.Bool("update-dataset", false, "replace fixtureCommit values in the dataset")
	flag.Parse()

	if strings.TrimSpace(*fixtureRepository) == "" {
		fatal(errors.New("--fixture-repository is required"))
	}
	ctx := context.Background()
	dataset, err := eval.Load(eval.SoftwareV1)
	if err != nil {
		fatal(err)
	}
	commits, err := buildRepository(ctx, *fixtureRepository, *templateDirectory, dataset)
	if err != nil {
		fatal(err)
	}
	if err := writeLock(*lockPath, dataset, commits); err != nil {
		fatal(err)
	}
	if *updateDataset {
		if err := writeDataset(*datasetPath, dataset, commits); err != nil {
			fatal(err)
		}
	}
	fmt.Printf("created %d immutable fixture commits in %s\n", len(commits), *fixtureRepository)
}

func buildRepository(ctx context.Context, repositoryPath, templatePath string, dataset eval.Dataset) (map[string]string, error) {
	absoluteRepository, err := filepath.Abs(repositoryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve fixture repository: %w", err)
	}
	if _, err := os.Stat(absoluteRepository); err == nil {
		return nil, fmt.Errorf("refusing to overwrite existing fixture repository %s", absoluteRepository)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect fixture repository: %w", err)
	}
	if err := os.MkdirAll(absoluteRepository, 0o755); err != nil {
		return nil, fmt.Errorf("create fixture repository: %w", err)
	}
	if err := copyTree(templatePath, absoluteRepository); err != nil {
		return nil, err
	}
	if err := runGit(ctx, absoluteRepository, nil, "init", "-b", "main"); err != nil {
		return nil, err
	}
	for _, args := range [][]string{{"config", "user.name", "ForgeFlow Fixture Builder"}, {"config", "user.email", "fixture-builder@forgeflow.invalid"}, {"config", "core.autocrlf", "false"}} {
		if err := runGit(ctx, absoluteRepository, nil, args...); err != nil {
			return nil, err
		}
	}
	baseEnv := commitEnvironment(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC))
	if err := runGit(ctx, absoluteRepository, baseEnv, "add", "--all"); err != nil {
		return nil, err
	}
	if err := runGit(ctx, absoluteRepository, baseEnv, "commit", "-m", "chore: establish software v1 fixture base"); err != nil {
		return nil, err
	}
	base, err := gitOutput(ctx, absoluteRepository, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}

	commits := make(map[string]string, len(dataset.Cases))
	for index, evalCase := range dataset.Cases {
		if err := runGit(ctx, absoluteRepository, nil, "switch", "--detach", base); err != nil {
			return nil, err
		}
		branch := "fixture/software-v1/" + evalCase.ID
		if err := runGit(ctx, absoluteRepository, nil, "switch", "-c", branch); err != nil {
			return nil, err
		}
		descriptor := caseDescriptor{ID: evalCase.ID, Category: evalCase.Category, Task: evalCase.Task, ExpectedDecision: evalCase.ExpectedDecision}
		data, err := json.MarshalIndent(descriptor, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode case %s: %w", evalCase.ID, err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(absoluteRepository, "CASE.json"), data, 0o644); err != nil {
			return nil, fmt.Errorf("write case %s: %w", evalCase.ID, err)
		}
		commitEnv := commitEnvironment(time.Date(2026, 8, 30, 0, index+1, 0, 0, time.UTC))
		if err := runGit(ctx, absoluteRepository, commitEnv, "add", "CASE.json"); err != nil {
			return nil, err
		}
		if err := runGit(ctx, absoluteRepository, commitEnv, "commit", "-m", "fixture: "+evalCase.ID); err != nil {
			return nil, err
		}
		sha, err := gitOutput(ctx, absoluteRepository, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		commits[evalCase.ID] = sha
		if err := runGit(ctx, absoluteRepository, nil, "tag", "software-v1/"+evalCase.ID, sha); err != nil {
			return nil, err
		}
	}
	if err := runGit(ctx, absoluteRepository, nil, "switch", "main"); err != nil {
		return nil, err
	}
	return commits, nil
}

func copyTree(source, destination string) error {
	absoluteSource, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve fixture template: %w", err)
	}
	return filepath.WalkDir(absoluteSource, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(absoluteSource, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func writeLock(path string, dataset eval.Dataset, commits map[string]string) error {
	lock := lockFile{Dataset: dataset.Name, Version: dataset.Version, Cases: make([]lockEntry, 0, len(dataset.Cases))}
	for _, evalCase := range dataset.Cases {
		lock.Cases = append(lock.Cases, lockEntry{ID: evalCase.ID, Commit: commits[evalCase.ID], Ref: "refs/tags/software-v1/" + evalCase.ID})
	}
	return writeJSON(path, lock)
}

func writeDataset(path string, dataset eval.Dataset, commits map[string]string) error {
	for index := range dataset.Cases {
		dataset.Cases[index].FixtureCommit = commits[dataset.Cases[index].ID]
	}
	return writeJSON(path, dataset)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func commitEnvironment(timestamp time.Time) []string {
	value := timestamp.Format(time.RFC3339)
	return []string{"GIT_AUTHOR_DATE=" + value, "GIT_COMMITTER_DATE=" + value}
}

func runGit(ctx context.Context, directory string, extraEnvironment []string, args ...string) error {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	command.Env = append(os.Environ(), extraEnvironment...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fixture builder:", err)
	os.Exit(1)
}
