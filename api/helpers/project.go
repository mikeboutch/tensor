package helpers

import (
	"gopkg.in/mgo.v2/bson"
	"bitbucket.pearson.com/apseng/tensor/db"
)

func IsUniqueProject(name string, OID bson.ObjectId) bool {
	count, err := db.Projects().Find(bson.M{"name": name, "organization_id": OID}).Count();
	if err == nil && count > 0 {
		return false
	}

	return true
}

func IsNotUniqueProject(name string, OID bson.ObjectId) bool {
	count, err := db.Projects().Find(bson.M{"name": name, "organization_id": OID}).Count();
	if err == nil && count > 0 {
		return true
	}

	return false
}

func _projectExist(ID bson.ObjectId) bool {
	count, err := db.Projects().FindId(ID).Count();
	if err == nil && count > 0 {
		return true
	}
	return false
}

func ProjectExist(ID bson.ObjectId) bool {
	if _credentialExist(ID) {
		return true
	}
	return false
}