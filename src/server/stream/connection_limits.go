package stream

import "sync"

type streamConnectionLimiter struct {
	maxPerAccount int
	maxPerSession int

	mu            sync.Mutex
	byAccount     map[uint]int
	byAccountSess map[uint]map[uint]int
}

func newStreamConnectionLimiter(maxPerAccount int, maxPerSession int) *streamConnectionLimiter {
	if maxPerAccount <= 0 {
		maxPerAccount = 1
	}
	if maxPerSession <= 0 {
		maxPerSession = 1
	}

	return &streamConnectionLimiter{
		maxPerAccount: maxPerAccount,
		maxPerSession: maxPerSession,
		byAccount:     make(map[uint]int),
		byAccountSess: make(map[uint]map[uint]int),
	}
}

func (l *streamConnectionLimiter) tryAcquire(accountID uint, sessionID uint) bool {
	if l == nil {
		return true
	}
	if accountID == 0 || sessionID == 0 {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	accountCount := l.byAccount[accountID]
	if accountCount >= l.maxPerAccount {
		return false
	}

	sessionMap, ok := l.byAccountSess[accountID]
	if !ok {
		sessionMap = make(map[uint]int)
		l.byAccountSess[accountID] = sessionMap
	}

	sessionCount := sessionMap[sessionID]
	if sessionCount >= l.maxPerSession {
		return false
	}

	l.byAccount[accountID] = accountCount + 1
	sessionMap[sessionID] = sessionCount + 1
	return true
}

func (l *streamConnectionLimiter) release(accountID uint, sessionID uint) {
	if l == nil {
		return
	}
	if accountID == 0 || sessionID == 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if current := l.byAccount[accountID]; current > 1 {
		l.byAccount[accountID] = current - 1
	} else {
		delete(l.byAccount, accountID)
	}

	sessionMap, ok := l.byAccountSess[accountID]
	if !ok {
		return
	}

	if current := sessionMap[sessionID]; current > 1 {
		sessionMap[sessionID] = current - 1
	} else {
		delete(sessionMap, sessionID)
	}

	if len(sessionMap) == 0 {
		delete(l.byAccountSess, accountID)
	}
}
