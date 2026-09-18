package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Poll struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Question  string        `bson:"question" json:"question"`
	Options   []string      `bson:"options" json:"options"`
	Votes     []int         `bson:"votes" json:"votes"`
	CreatedBy string        `bson:"createdBy" json:"createdBy"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt" json:"updatedAt"`
}
