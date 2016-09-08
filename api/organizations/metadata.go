package organizations

import (
	"gopkg.in/mgo.v2/bson"
	"github.com/gin-gonic/gin"
	"bitbucket.pearson.com/apseng/tensor/models"
	"bitbucket.pearson.com/apseng/tensor/db"
)



// Create a new organization
func setMetadata(o *models.Organization) error {

	ID := o.ID.Hex()
	o.Type = "organization"
	o.Url = "/v1/organizations/" + ID + "/"
	o.Related = gin.H{
		"created_by": "/v1/users/" + o.CreatedBy.Hex() + "/",
		"modified_by": "/v1/users/" + o.ModifiedBy.Hex() + "/",
		"notification_templates_error": "/v1/organizations/" + ID + "/notification_templates_error/",
		"notification_templates_success": "/v1/organizations/" + ID + "/notification_templates_success/",
		"users": "/v1/organizations/" + ID + "/users/",
		"object_roles": "/v1/organizations/" + ID + "/object_roles/",
		"notification_templates_any":  "/v1/organizations/" + ID + "/notification_templates_any/",
		"teams": "/v1/organizations/" + ID + "/teams/",
		"access_list": "/v1/organizations/" + ID + "/access_list/",
		"notification_templates": "/v1/organizations/" + ID + "/notification_templates/",
		"admins": "/v1/organizations/" + ID + "/admins/",
		"credentials": "/v1/organizations/" + ID + "/credentials/",
		"inventories":  "/v1/organizations/" + ID + "/inventories/",
		"activity_stream": "/v1/organizations/" + ID + "/activity_stream/",
		"projects": "/v1/organizations/" + ID + "/projects/",
	}

	if err := setSummaryFields(o); err != nil {
		return err
	}

	return nil
}

func setSummaryFields(o *models.Organization) error {

	dbu := db.MongoDb.C(models.DBC_USERS)
	dbacl := db.MongoDb.C(models.DBC_ACl)

	var modified models.User
	var created models.User
	var owners []models.User

	if err := dbu.FindId(o.CreatedBy).One(&created); err != nil {
		return err
	}

	if err := dbu.FindId(o.ModifiedBy).One(&modified); err != nil {
		return err
	}

	q := []bson.M{
		{"$match": bson.M{"object": o.ID, "role": "admin", }},
		{
			"$lookup": bson.M{
				"from": "users",
				"localField": "user_id",
				"foreignField": "_id",
				"as": "owner",
			},
		},
		{"$project": bson.M{"_id":0, "users":bson.M{"$arrayElemAt": []interface{}{"$owner", 0} }, }},
		{"$project": bson.M{
			"_id":"$users._id",
			"created":"$users.created",
			"email":"$users.email",
			"name":"$users.name",
			"password":"$users.password",
			"username":"$users.username",
		}},
	}

	if err := dbacl.Pipe(q).All(&owners); err != nil {
		return err
	}

	//TODO: include teams to owners list

	o.SummaryFields = gin.H{
		"object_roles": []gin.H{
			{
				"Description": "Can view all settings for the organization",
				"Name":"auditor",
			},
			{
				"Description":"Can manage all aspects of the organization",
				"Name":"admin",
			},
			{
				"Description":"User is a member of the organization",
				"Name":"member",
			},
			{
				"Description":"May view settings for the organization",
				"Name":"read",
			},
		},

		"related_field_counts": map[string]int{
			"job_templates":1,
			"users":2,
			"teams":2,
			"admins":2,
			"inventories":1,
			"projects":1,
		},
		"created_by": gin.H{
			"id":         created.ID,
			"username":   created.Username,
			"first_name": created.FirstName,
			"last_name":  created.LastName,
		},
		"modified_by": gin.H{
			"id":         modified.ID,
			"username":   modified.Username,
			"first_name": modified.FirstName,
			"last_name":  modified.LastName,
		},
		"owners": owners,
	}

	return nil
}