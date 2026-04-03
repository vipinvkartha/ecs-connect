package tui

// RunMode distinguishes ECS Exec from DynamoDB query completion.
type RunMode int

const (
	ModeECS RunMode = iota
	ModeDynamoDB
)

// Outcome is returned from Run after the user finishes or cancels (check error).
type Outcome struct {
	Mode   RunMode
	ECS    *Result
	Dynamo *DynamoOutcome
}

// DynamoOutcome holds the printed query result.
type DynamoOutcome struct {
	Table string
	JSON  string
}
