package planner

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"text/template"

	"forgeflow/internal/apperror"
)

//go:embed prompts/*/*
var embeddedPrompts embed.FS

var promptVersionPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*/v[1-9][0-9]*$`)

type Prompt struct {
	Version      string
	System       string
	UserTemplate string
	SHA256       string
}

type PromptLoader struct {
	files fs.FS
}

func NewPromptLoader(files fs.FS) *PromptLoader {
	if files == nil {
		files = embeddedPrompts
	}
	return &PromptLoader{files: files}
}

func (l *PromptLoader) Load(version string) (Prompt, error) {
	if !promptVersionPattern.MatchString(version) {
		return Prompt{}, apperror.New(apperror.CodeValidation, "prompt version must look like planner/v1")
	}
	base := "prompts/" + version
	system, err := fs.ReadFile(l.files, base+"/system.txt")
	if err != nil {
		return Prompt{}, apperror.Wrap(err, apperror.CodeNotFound, "planner.prompt.system", "system prompt version was not found")
	}
	user, err := fs.ReadFile(l.files, base+"/user.tmpl")
	if err != nil {
		return Prompt{}, apperror.Wrap(err, apperror.CodeNotFound, "planner.prompt.user", "user prompt version was not found")
	}
	if len(system) == 0 || len(user) == 0 || len(system)+len(user) > 128*1024 {
		return Prompt{}, apperror.New(apperror.CodeValidation, "prompt files are empty or exceed the size limit")
	}
	digest := sha256.Sum256(append(append([]byte(nil), system...), user...))
	return Prompt{
		Version: version, System: strings.TrimSpace(string(system)), UserTemplate: string(user),
		SHA256: hex.EncodeToString(digest[:]),
	}, nil
}

type PromptData struct {
	Task              string
	RepositoryPath    string
	BaseRevision      string
	RepositoryContext string
}

func (p Prompt) RenderUser(data PromptData) (string, error) {
	jsonString := func(value string) (string, error) {
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}
	tmpl, err := template.New(p.Version).Funcs(template.FuncMap{"jsonString": jsonString}).Option("missingkey=error").Parse(p.UserTemplate)
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeInternal, "planner.prompt.parse", "planner prompt template is invalid")
	}
	var output strings.Builder
	if err := tmpl.Execute(&output, data); err != nil {
		return "", apperror.Wrap(err, apperror.CodeInternal, "planner.prompt.render", "planner prompt could not be rendered")
	}
	if output.Len() > 256*1024 {
		return "", apperror.New(apperror.CodePolicyDenied, "rendered planner prompt exceeds the size limit")
	}
	return output.String(), nil
}

func (p Prompt) String() string {
	return fmt.Sprintf("Prompt(version=%s, sha256=%s)", p.Version, p.SHA256)
}
