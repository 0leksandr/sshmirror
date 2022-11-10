package main

import (
	"encoding/json"
	"github.com/0leksandr/my.go"
	"time"
)

const TableModifications = "modifications"
const ColumnTime = "time"
const ColumnModification = "modification"

type Archive struct {
	db my.DB
}
func (Archive) New(db my.DB) Archive {
	return Archive{db}
}
func (archive Archive) Save(modifications <-chan Modification) {
	for modification := range modifications {
		archive.db.Insert( // TODO: ensure `Close`'ing and insert with batches
			TableModifications,
			map[string]interface{}{
				ColumnTime:         time.Now().Unix(),
				ColumnModification: jsonSerialize(modification.Serialize()),
			},
		)
	}
}
func (archive Archive) Load(from time.Time) []Modification {
	serialized := archive.db.SelectMany(
		TableModifications,
		[]string{ColumnModification},
		map[string]interface{}{
			ColumnTime + " >= ?": from.Unix(),
		},
		nil,
	)
	deserialized := make([]Modification, 0, len(serialized))
	for _, row := range serialized {
		deserialized = append(
			deserialized,
			deserializeModification(decodeToSerialized(jsonDeserialize(row[ColumnModification].(string)))),
		)
	}
	return deserialized
}
func (archive Archive) Delete(from, to time.Time) {
	archive.db.Delete(
		TableModifications,
		map[string]interface{}{
			ColumnTime + " >= ?": from.Unix(),
			ColumnTime + " <= ?": to.Unix(),
		},
	)
}

func jsonSerialize(v interface{}) string {
	res, err := json.Marshal(v)
	PanicIf(err)
	return string(res)
}
func jsonDeserialize(_json string) interface{} {
	var deserialized interface{}
	Must(json.Unmarshal([]byte(_json), &deserialized))
	return deserialized
}
