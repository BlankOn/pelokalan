package service

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"

	"pelokalan/internal/glossary"
	"pelokalan/internal/models"
)

// Module groups synced from a GitHub repository branch (e.g. python/python-docs-id, branch 3.14):
// the branch is downloaded as one archive from GitHub's download host (no API calls, no key), every
// PO file keeps its path in the repository (repo_path) and gets a flat file name for Pelokalan, and
// members' changes go back as a git patch (CatalogService.GitPatch).

// GitTermMaxWords: from repository groups (documentation) only strings this short become glossary terms
const GitTermMaxWords = 12

var (
	gitNameRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	gitBranchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
)

// ParseGitHubRepo reads a repository given as "owner/name", "https://github.com/owner/name" (".git"
// allowed) or a branch address "https://github.com/owner/name/tree/branch"; branch (when given) wins
// over the one in the address. It returns "owner/name" and the branch.
func ParseGitHubRepo(raw, branch string) (repo, br string, ok bool) {
	raw = strings.TrimSpace(raw)
	br = strings.TrimSpace(branch)
	rest := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(u.Host, "github.com") && !strings.EqualFold(u.Host, "www.github.com") {
			return "", "", false
		}
		rest = u.Path
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner, name := parts[0], strings.TrimSuffix(parts[1], ".git")
	if len(parts) >= 4 && parts[2] == "tree" && br == "" {
		br = strings.Join(parts[3:], "/")
	} else if len(parts) > 2 && parts[2] != "tree" {
		return "", "", false
	}
	if !gitNameRe.MatchString(owner) || !gitNameRe.MatchString(name) || !gitBranchRe.MatchString(br) ||
		strings.Contains(br, "..") || strings.HasPrefix(br, "/") || strings.HasSuffix(br, "/") {
		return "", "", false
	}
	return owner + "/" + name, br, true
}

// GitHubArchiveURL is the download address of a branch as a .tar.gz archive
func GitHubArchiveURL(repo, branch string) string {
	return "https://codeload.github.com/" + repo + "/tar.gz/refs/heads/" + branch
}

// gitFiles turns the files of a branch archive into repository files: the archive's top folder
// ("python-docs-id-3.14/") is dropped from the path, and the name becomes the flat file name
func gitFiles(files []glossary.File) []glossary.File {
	out := make([]glossary.File, 0, len(files))
	for _, f := range files {
		p := f.Path
		if i := strings.Index(p, "/"); i >= 0 {
			p = p[i+1:]
		}
		if p == "" {
			continue
		}
		f.Path = p
		f.Name = RepoFileName(p)
		out = append(out, f)
	}
	return out
}

// RepoFileName is the flat Pelokalan file name of a repository path: folders joined with "__" and dots
// inside the name turned into dashes, so the module (the name before the first dot) is unique:
// "library/os.path.po" → "library__os-path.po", "tutorial/index.po" → "tutorial__index.po"
func RepoFileName(p string) string {
	ext := path.Ext(p) // ".po"
	stem := strings.TrimSuffix(p, ext)
	stem = strings.ReplaceAll(stem, ".", "-")
	return strings.ReplaceAll(stem, "/", "__") + ext
}

// IsGitProject tells whether a project's files come from git repositories only (it has module groups
// and every one is synced from GitHub): files then come from the repository, so the Penerjemahan page
// has no template section and no manual upload. A project that also has other groups keeps both.
func (s *GlossaryService) IsGitProject(ctx context.Context, p *models.Project) bool {
	groups, err := s.Groups(ctx, p)
	if err != nil || len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if !g.IsGit() {
			return false
		}
	}
	return true
}

// GitGroupFor returns the module group a repository file was last synced from (nil = unknown)
func (s *GlossaryService) GitGroupFor(ctx context.Context, p *models.Project, repoPath string) *models.ModuleGroup {
	if repoPath == "" {
		return nil
	}
	if _, g, err := s.repo.ProjectFileByRepoPath(ctx, p.ID, repoPath); err == nil && g != nil && g.IsGit() {
		return g
	}
	return nil
}

// GitBranchName is the local branch a file's patch goes on: one per module ("library/os.po" →
// "pelokalan/library-os"), so each file becomes its own pull request
func GitBranchName(repoPath string) string {
	name := strings.TrimSuffix(repoPath, ".po")
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(strings.ReplaceAll(b.String(), "..", "-"), "-.")
	if out == "" {
		out = "file"
	}
	return "pelokalan/" + out
}

// GitFileURL is a repository file's address on GitHub ("" when its group is unknown)
func (s *GlossaryService) GitFileURL(ctx context.Context, p *models.Project, repoPath string) string {
	if repoPath == "" {
		return ""
	}
	if _, g, err := s.repo.ProjectFileByRepoPath(ctx, p.ID, repoPath); err == nil && g != nil {
		return g.GitFileURL(repoPath)
	}
	return ""
}
