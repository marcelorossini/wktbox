package prune

type Candidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Worktree string `json:"worktree"`
}

type Warning struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Report struct {
	Candidates []Candidate `json:"candidates"`
	Destroyed  []Candidate `json:"destroyed"`
	Warnings   []Warning   `json:"warnings,omitempty"`
}
