package errors

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// ErrorHandler provides centralized error handling with classification and reporting
// Keys for the classified-error fields in debug logs and JSON reports.
const (
	fieldCategory = "category"
	fieldSeverity = "severity"
	fieldMessage  = "message"
)

type ErrorHandler struct {
	classifier *ErrorClassifier
	stats      *ErrorStatistics
	mu         sync.RWMutex
}

// ErrorStatistics tracks error patterns and frequencies
type ErrorStatistics struct {
	TotalErrors       int                   `json:"total_errors"`
	ErrorsByCategory  map[ErrorCategory]int `json:"errors_by_category"`
	ErrorsBySeverity  map[ErrorSeverity]int `json:"errors_by_severity"`
	RecoverableErrors int                   `json:"recoverable_errors"`
	RetryAttempts     int                   `json:"retry_attempts"`
	LastError         *ClassifiedError      `json:"last_error,omitempty"`
	ErrorHistory      []*ClassifiedError    `json:"error_history,omitempty"`
	StartTime         time.Time             `json:"start_time"`
}

// NewErrorHandler creates a new error handler with classification
func NewErrorHandler() *ErrorHandler {
	return &ErrorHandler{
		classifier: NewErrorClassifier(),
		stats: &ErrorStatistics{
			ErrorsByCategory: make(map[ErrorCategory]int),
			ErrorsBySeverity: make(map[ErrorSeverity]int),
			StartTime:        time.Now(),
		},
	}
}

// HandleError processes an error with full classification and logging
func (eh *ErrorHandler) HandleError(
	ctx context.Context, err error, operation string, errContext map[string]interface{},
) *ClassifiedError {
	if err == nil {
		return nil
	}

	// Add operation context
	if errContext == nil {
		errContext = make(map[string]interface{})
	}
	errContext["operation"] = operation
	errContext["timestamp"] = time.Now().Format(time.RFC3339)

	// Classify the error
	classified := eh.classifier.Classify(err, errContext)

	// Update statistics
	eh.updateStatistics(classified)

	// Log the classified error
	eh.logClassifiedError(classified)

	return classified
}

// HandleErrorWithRetry handles an error and provides retry logic if appropriate
func (eh *ErrorHandler) HandleErrorWithRetry(
	ctx context.Context, err error, operation string, errContext map[string]interface{}, attempt int,
) (*ClassifiedError, bool) {
	classified := eh.HandleError(ctx, err, operation, errContext)
	if classified == nil {
		return nil, false
	}

	// Check if retry is appropriate
	shouldRetry := eh.shouldRetry(classified, attempt)
	if shouldRetry {
		eh.mu.Lock()
		eh.stats.RetryAttempts++
		eh.mu.Unlock()

		debug.Info("Error handler: Retry recommended",
			debug.F(fieldCategory, classified.Category),
			debug.F("attempt", attempt),
			debug.F("retryAfter", classified.RetryAfter))
	}

	return classified, shouldRetry
}

// shouldRetry determines if an operation should be retried
func (eh *ErrorHandler) shouldRetry(classified *ClassifiedError, attempt int) bool {
	// Maximum retry attempts
	maxRetries := 3
	if attempt >= maxRetries {
		return false
	}

	// Only retry recoverable errors
	if !classified.Recoverable {
		return false
	}

	// Category-specific retry logic
	switch classified.Category {
	case CategoryTimeout, CategoryConnection:
		return attempt < 3
	case CategoryServerUnavailable:
		return attempt < 2
	case CategoryTransport:
		return attempt < 2
	default:
		return false
	}
}

// GetRetryDelay returns the delay before next retry attempt
func (eh *ErrorHandler) GetRetryDelay(classified *ClassifiedError, attempt int) time.Duration {
	if classified.RetryAfter != nil {
		// Use exponential backoff: base delay * 2^attempt
		multiplier := int64(1 << uint(attempt))
		return time.Duration(int64(*classified.RetryAfter) * multiplier)
	}

	// Default exponential backoff
	return time.Duration(1000*attempt*attempt) * time.Millisecond
}

// updateStatistics updates error tracking statistics
func (eh *ErrorHandler) updateStatistics(classified *ClassifiedError) {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	eh.stats.TotalErrors++
	eh.stats.ErrorsByCategory[classified.Category]++
	eh.stats.ErrorsBySeverity[classified.Severity]++

	if classified.Recoverable {
		eh.stats.RecoverableErrors++
	}

	eh.stats.LastError = classified

	// Keep limited error history (last 50 errors)
	if len(eh.stats.ErrorHistory) >= 50 {
		eh.stats.ErrorHistory = eh.stats.ErrorHistory[1:]
	}
	eh.stats.ErrorHistory = append(eh.stats.ErrorHistory, classified)
}

// logClassifiedError logs the error with appropriate level and detail
func (eh *ErrorHandler) logClassifiedError(classified *ClassifiedError) {
	fields := []debug.Field{
		debug.F(fieldCategory, classified.Category),
		debug.F(fieldSeverity, classified.Severity),
		debug.F("recoverable", classified.Recoverable),
		debug.F(fieldMessage, classified.Message),
	}

	if classified.Context != nil {
		for key, value := range classified.Context {
			fields = append(fields, debug.F(key, value))
		}
	}

	if classified.RetryAfter != nil {
		fields = append(fields, debug.F("retryAfter", *classified.RetryAfter))
	}

	// The classified error is returned to the caller, which reports it;
	// logging it above debug would print every failure twice.
	debug.Debug("Classified "+classified.Severity.String()+": "+classified.Message, fields...)
}

// GetStatistics returns current error statistics
func (eh *ErrorHandler) GetStatistics() *ErrorStatistics {
	eh.mu.RLock()
	defer eh.mu.RUnlock()

	// Return a copy to avoid race conditions
	stats := &ErrorStatistics{
		TotalErrors:       eh.stats.TotalErrors,
		ErrorsByCategory:  make(map[ErrorCategory]int),
		ErrorsBySeverity:  make(map[ErrorSeverity]int),
		RecoverableErrors: eh.stats.RecoverableErrors,
		RetryAttempts:     eh.stats.RetryAttempts,
		LastError:         eh.stats.LastError,
		StartTime:         eh.stats.StartTime,
	}

	for k, v := range eh.stats.ErrorsByCategory {
		stats.ErrorsByCategory[k] = v
	}
	for k, v := range eh.stats.ErrorsBySeverity {
		stats.ErrorsBySeverity[k] = v
	}

	// Copy recent error history (last 10)
	historyCount := len(eh.stats.ErrorHistory)
	if historyCount > 10 {
		stats.ErrorHistory = make([]*ClassifiedError, 10)
		copy(stats.ErrorHistory, eh.stats.ErrorHistory[historyCount-10:])
	} else {
		stats.ErrorHistory = make([]*ClassifiedError, historyCount)
		copy(stats.ErrorHistory, eh.stats.ErrorHistory)
	}

	return stats
}

// GetErrorReport generates a detailed error report
func (eh *ErrorHandler) GetErrorReport() map[string]interface{} {
	stats := eh.GetStatistics()

	categories := make(map[string]int)
	severities := make(map[string]int)
	report := map[string]interface{}{
		"summary": map[string]interface{}{
			"total_errors":       stats.TotalErrors,
			"recoverable_errors": stats.RecoverableErrors,
			"retry_attempts":     stats.RetryAttempts,
			"uptime":             time.Since(stats.StartTime).String(),
		},
		"categories": categories,
		"severities": severities,
	}

	// Convert enum keys to strings
	for category, count := range stats.ErrorsByCategory {
		categories[category.String()] = count
	}
	for severity, count := range stats.ErrorsBySeverity {
		severities[severity.String()] = count
	}

	// Add last error details
	if stats.LastError != nil {
		lastError := map[string]interface{}{
			fieldCategory: stats.LastError.Category.String(),
			fieldSeverity: stats.LastError.Severity.String(),
			fieldMessage:  stats.LastError.Message,
			"recoverable": stats.LastError.Recoverable,
		}
		report["last_error"] = lastError

		if stats.LastError.Context != nil {
			lastError["context"] = stats.LastError.Context
		}
	}

	// Add recent error patterns
	if len(stats.ErrorHistory) > 0 {
		recentErrors := make([]map[string]interface{}, 0, len(stats.ErrorHistory))
		for _, err := range stats.ErrorHistory {
			recentErrors = append(recentErrors, map[string]interface{}{
				fieldCategory: err.Category.String(),
				fieldSeverity: err.Severity.String(),
				fieldMessage:  err.Message,
			})
		}
		report["recent_errors"] = recentErrors
	}

	return report
}

// ResetStatistics clears error statistics
func (eh *ErrorHandler) ResetStatistics() {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	eh.stats = &ErrorStatistics{
		ErrorsByCategory: make(map[ErrorCategory]int),
		ErrorsBySeverity: make(map[ErrorSeverity]int),
		StartTime:        time.Now(),
	}
}

// userFriendlyError is the error CreateUserFriendlyError returns: its text
// is the classification's explanation, the underlying error and the recovery
// suggestions, and it unwraps to the ClassifiedError, whose Unwrap reaches
// the original error. errors.As / errors.Is therefore still find what the
// failure carried (a *jsonrpc.Error, a named *debug.MCPError, a net error).
type userFriendlyError struct {
	message    string
	classified *ClassifiedError
}

func (e *userFriendlyError) Error() string { return e.message }

func (e *userFriendlyError) Unwrap() error { return e.classified }

// CreateUserFriendlyError creates a user-friendly error message with recovery suggestions
func (eh *ErrorHandler) CreateUserFriendlyError(classified *ClassifiedError) error {
	if classified == nil {
		return nil
	}

	// Main error message: the classification's explanation, followed by
	// the underlying error unless the explanation already quotes it -- the
	// explanation alone hides what the server actually said.
	message := classified.Message
	if cause := classified.Cause; cause != nil && !strings.Contains(message, cause.Error()) {
		message += ": " + cause.Error()
	}

	// Add recovery suggestions
	actions := eh.classifier.GetRecoveryActions(classified)
	if len(actions) > 0 {
		message += "\n\nSuggested actions:"
		for _, action := range actions {
			message += "\n  • " + action
		}
	}

	// Add retry information for recoverable errors
	if classified.Recoverable && classified.RetryAfter != nil {
		message += fmt.Sprintf("\n\nThis error may be temporary. Retry recommended after %v.", *classified.RetryAfter)
	}

	// Add context information if available
	if errContext := classified.Context; errContext != nil {
		if operation, ok := errContext["operation"].(string); ok {
			message = fmt.Sprintf("Operation '%s' failed: %s", operation, message)
		}
	}

	return &userFriendlyError{message: message, classified: classified}
}

// FormatErrorForJSON formats a classified error for JSON serialization
func (eh *ErrorHandler) FormatErrorForJSON(classified *ClassifiedError) map[string]interface{} {
	if classified == nil {
		return nil
	}

	result := map[string]interface{}{
		fieldCategory: classified.Category.String(),
		fieldSeverity: classified.Severity.String(),
		fieldMessage:  classified.Message,
		"recoverable": classified.Recoverable,
	}

	if classified.RetryAfter != nil {
		result["retry_after"] = classified.RetryAfter.String()
	}

	if classified.Context != nil {
		result["context"] = classified.Context
	}

	if classified.Cause != nil {
		result["cause"] = classified.Cause.Error()
	}

	// Add recovery actions
	actions := eh.classifier.GetRecoveryActions(classified)
	if len(actions) > 0 {
		result["recovery_actions"] = actions
	}

	return result
}
