package application

type WorkspaceFactory interface {
	Open(name string) Workspace
}
