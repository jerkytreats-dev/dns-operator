package certificate

import (
	"errors"
	"time"
)

type FailureClass string

const (
	FailureClassDNSPreflight FailureClass = "DNSPreflightFailed"
	FailureClassRateLimited  FailureClass = "RateLimited"
	FailureClassIssue        FailureClass = "IssueFailed"
)

type IssueError struct {
	Class FailureClass
	Err   error
}

func (e *IssueError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *IssueError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WrapIssueError(class FailureClass, err error) error {
	if err == nil {
		return nil
	}
	var issueErr *IssueError
	if errors.As(err, &issueErr) {
		return err
	}
	return &IssueError{Class: class, Err: err}
}

func FailureClassFromError(err error) FailureClass {
	var issueErr *IssueError
	if errors.As(err, &issueErr) && issueErr.Class != "" {
		return issueErr.Class
	}
	if IsRateLimitError(err) {
		return FailureClassRateLimited
	}
	return FailureClassIssue
}

func CooldownForFailure(class FailureClass, consecutiveFailures int32) time.Duration {
	if consecutiveFailures < 1 {
		consecutiveFailures = 1
	}

	var base time.Duration
	var max time.Duration
	switch class {
	case FailureClassDNSPreflight:
		base = 15 * time.Minute
		max = 2 * time.Hour
	case FailureClassRateLimited:
		base = 6 * time.Hour
		max = 48 * time.Hour
	default:
		base = 30 * time.Minute
		max = 6 * time.Hour
	}

	delay := base
	for i := int32(1); i < consecutiveFailures; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	if delay > max {
		return max
	}
	return delay
}
