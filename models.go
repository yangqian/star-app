package main

import "time"

type User struct {
	ID           int
	Username     string
	PasswordHash string
	IsAdmin      bool
}

type Star struct {
	ID            int
	UserID        int
	Username      string
	ReasonID      *int
	ReasonText    string
	Reason        string
	Stars         int
	AwardedBy     int
	AwardedByName string
	CreatedAt     time.Time
}

type Reason struct {
	ID           int
	Key          string
	Translations map[string]string
	Count        int
	Stars        int
}

type APIKey struct {
	ID        int
	KeyHash   string
	Label     string
	CreatedAt time.Time
}

type SessionData struct {
	UserID  int
	IsAdmin bool
}

type Reward struct {
	ID   int
	Name string
	Cost int
	Icon string
}

type Redemption struct {
	ID         int
	UserID     int
	Username   string
	RewardName string
	Cost       int
	CreatedAt  time.Time
}
