package projects

import (
	"github.com/gin-gonic/gin"
	"bitbucket.pearson.com/apseng/tensor/models"
	"net/http"
	"gopkg.in/mgo.v2/bson"
	"time"
	"bitbucket.pearson.com/apseng/tensor/db"
	"log"
	"bitbucket.pearson.com/apseng/tensor/util"
	"strconv"
	"path/filepath"
	"os"
	"regexp"
	"bitbucket.pearson.com/apseng/tensor/controllers/metadata"
	"bitbucket.pearson.com/apseng/tensor/roles"
	"bitbucket.pearson.com/apseng/tensor/controllers/helpers"
	"strings"
	"bitbucket.pearson.com/apseng/tensor/runners"
	"github.com/gin-gonic/gin/binding"
	"io"
)

const _CTX_PROJECT = "project"
const _CTX_USER = "user"
const _CTX_PROJECT_ID = "project_id"

// ProjectMiddleware takes project_id parameter from gin.Context and
// fetches project data from the database
// it set project data under key project in gin.Context
func Middleware(c *gin.Context) {
	ID, err := util.GetIdParam(_CTX_PROJECT_ID, c)

	if err != nil {
		log.Print("Error while getting the Project:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Not Found"},
		})
		c.Abort()
		return
	}

	var project models.Project
	err = db.Projects().FindId(bson.ObjectIdHex(ID)).One(&project);
	if err != nil {
		log.Print("Error while getting the Project:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Not Found"},
		})
		c.Abort()
		return
	}

	c.Set(_CTX_PROJECT, project)
	c.Next()
}

// GetProject returns the project as a JSON object
func GetProject(c *gin.Context) {
	project := c.MustGet(_CTX_PROJECT).(models.Project)
	metadata.ProjectMetadata(&project)

	// send response with JSON rendered data
	c.JSON(http.StatusOK, project)
}

// GetProjects returns a JSON array of projects
func GetProjects(c *gin.Context) {
	user := c.MustGet(_CTX_USER).(models.User)

	parser := util.NewQueryParser(c)
	match := bson.M{}
	match = parser.Match([]string{"type", "status"}, match)
	match = parser.Lookups([]string{"name"}, match)

	query := db.Projects().Find(match)
	if order := parser.OrderBy(); order != "" {
		query.Sort(order)
	}

	var projects []models.Project
	// new mongodb iterator
	iter := query.Iter()
	// loop through each result and modify for our needs
	var tmpProject models.Project
	// iterate over all and only get valid objects
	for iter.Next(&tmpProject) {
		// if the user doesn't have access to credential
		// skip to next
		if !roles.ProjectRead(user, tmpProject) {
			continue
		}
		if err := metadata.ProjectMetadata(&tmpProject); err != nil {
			log.Println("Error while setting metatdata:", err)
			c.JSON(http.StatusInternalServerError, models.Error{
				Code:http.StatusInternalServerError,
				Messages: []string{"Error while getting Project"},
			})
			return
		}
		// good to go add to list
		projects = append(projects, tmpProject)
	}
	if err := iter.Close(); err != nil {
		log.Println("Error while retriving Project data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Project"},
		})
		return
	}

	count := len(projects)
	pgi := util.NewPagination(c, count)
	//if page is incorrect return 404
	if pgi.HasPage() {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Invalid page " + strconv.Itoa(pgi.Page()) + ": That page contains no results."})
		return
	}
	// send response with JSON rendered data
	c.JSON(http.StatusOK, models.Response{
		Count:count,
		Next: pgi.NextPage(),
		Previous: pgi.PreviousPage(),
		Results: projects[pgi.Skip():pgi.End()],
	})
}

// AddProject creates a new project
func AddProject(c *gin.Context) {
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.Project
	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	// check whether the organization exist or not
	if !helpers.OrganizationExist(req.OrganizationID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Organization does not exists."},
		})
		return
	}

	// if a project exists within the Organization, reject the request
	if helpers.IsNotUniqueProject(req.Name, req.OrganizationID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Project with this Name and Organization already exists."},
		})
		return
	}

	// check whether the scm credential exist or not
	if req.ScmCredentialID != nil {
		if !helpers.SCMCredentialExist(*req.ScmCredentialID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"SCM Credential does not exists."},
			})
			return
		}
	}

	// trim strings white space
	req.Name = strings.Trim(req.Name, " ")
	req.Description = strings.Trim(req.Description, " ")

	req.ID = bson.NewObjectId()
	req.LocalPath = "/opt/tensor/projects/" + req.ID.Hex()
	req.CreatedBy = user.ID
	req.ModifiedBy = user.ID
	req.Created = time.Now()
	req.Modified = time.Now()

	if err := db.Projects().Insert(req); err != nil {
		log.Println("Error while creating Project:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Project"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Project " + req.Name + " created")

	// before set metadata update the project
	if sysJobID, err := runners.UpdateProject(req); err != nil {
		log.Println("Error while scm update " + sysJobID.Job.ID.Hex(), err)
	}

	if err := metadata.ProjectMetadata(&req); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Project"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusCreated, req)
}


// UpdateProject will update the Project
func UpdateProject(c *gin.Context) {
	// get Project from the gin.Context
	project := c.MustGet(_CTX_PROJECT).(models.Project)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.Project
	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	// check whether the organization exist or not
	if !helpers.OrganizationExist(req.OrganizationID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Organization does not exists."},
		})
		return
	}

	if req.Name != project.Name {
		// if a project exists within the Organization, reject the request
		if helpers.IsNotUniqueProject(req.Name, req.OrganizationID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Project with this Name and Organization already exists."},
			})
			return
		}
	}

	// check whether the ScmCredential exist or not
	if req.ScmCredentialID != nil {
		if !helpers.SCMCredentialExist(*req.ScmCredentialID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"SCM Credential does not exists."},
			})
			return
		}
	}

	// trim strings white space
	req.Name = strings.Trim(req.Name, " ")
	req.Description = strings.Trim(req.Description, " ")

	req.ID = project.ID
	req.CreatedBy = project.CreatedBy
	req.ModifiedBy = user.ID
	req.Created = project.Created
	req.Modified = time.Now()

	// update object
	if err := db.Projects().UpdateId(project.ID, req); err != nil {
		log.Println("Error while updating Project:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while updating Project"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Project " + req.Name + " updated")

	// before set metadata update the project
	if sysJobID, err := runners.UpdateProject(req); err != nil {
		log.Println("Error while scm update " + sysJobID.Job.ID.Hex(), err)
	}

	// set `related` and `summary` feilds
	if err := metadata.ProjectMetadata(&req); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Project"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusOK, req)
}

// UpdateProject will update the Project
func PatchProject(c *gin.Context) {
	// get Project from the gin.Context
	project := c.MustGet(_CTX_PROJECT).(models.Project)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.PatchProject
	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	if len(req.OrganizationID) == 12 {
		// check whether the organization exist or not
		if !helpers.OrganizationExist(req.OrganizationID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Organization does not exists."},
			})
			return
		}
	}

	if req.Name != project.Name {
		ogID := project.OrganizationID
		if len(req.OrganizationID) == 12 {
			ogID = req.OrganizationID
		}
		// if a project exists within the Organization, reject the request
		if helpers.IsNotUniqueProject(req.Name, ogID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Project with this Name and Organization already exists."},
			})
			return
		}
	}

	// check whether the ScmCredential exist or not
	if req.ScmCredentialID != nil {
		if !helpers.SCMCredentialExist(*req.ScmCredentialID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"SCM Credential does not exists."},
			})
			return
		}
	}

	// trim strings white space
	req.Name = strings.Trim(req.Name, " ")
	req.Description = strings.Trim(req.Description, " ")

	req.ModifiedBy = user.ID
	req.Modified = time.Now()

	// update object
	if err := db.Projects().UpdateId(project.ID, bson.M{"$set": req}); err != nil {
		log.Println("Error while updating Project:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while updating Project"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(project.ID, user.ID, "Project " + req.Name + " updated")

	// get newly updated host
	var resp models.Project
	if err := db.Projects().FindId(project.ID).One(&resp); err != nil {
		log.Print("Error while getting the updated Project:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Error while getting the updated Project"},
		})
		return
	}

	// before set metadata update the project
	if sysJobID, err := runners.UpdateProject(resp); err != nil {
		log.Println("Error while scm update " + sysJobID.Job.ID.Hex(), err)
	}

	// set `related` and `summary` feilds
	if err := metadata.ProjectMetadata(&resp); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Project Information"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusOK, resp)
}

// RemoveProject will remove the Project
// from the db.DBC_PROJECTS collection
func RemoveProject(c *gin.Context) {
	// get Project from the gin.Context
	project := c.MustGet(_CTX_PROJECT).(models.Project)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	changes, err := db.Jobs().RemoveAll(bson.M{"project_id": project.ID})
	if err != nil {
		log.Println("Error while removing Project Jobs:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Project Jobs"},
		})
		return
	}

	log.Println("Jobs remove info:", changes.Removed)

	changes, err = db.JobTemplates().RemoveAll(bson.M{"project_id": project.ID})
	if err != nil {
		log.Println("Error while removing Project Job Templates:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Project Job Templates"},
		})
		return
	}

	log.Println("Job Template remove info:", changes.Removed)

	// remove object from the collection
	if err = db.Projects().RemoveId(project.ID); err != nil {
		log.Println("Error while removing Project:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Project"},
		})
		return
	}


	// cleanup directories from a concurrent thread
	go func() {
		if err := os.RemoveAll(project.LocalPath); err != nil {
			log.Println("An error occured while removing project directory", err.Error())
		}
	}()

	// add new activity to activity stream
	addActivity(project.ID, user.ID, "Project " + project.Name + " deleted")

	// abort with 204 status code
	c.AbortWithStatus(http.StatusNoContent)
}

func Playbooks(c *gin.Context) {
	// get Project from the gin.Context
	project := c.MustGet(_CTX_PROJECT).(models.Project)

	files := []string{}
	err := filepath.Walk(project.LocalPath, func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			r, err := regexp.MatchString(".yml", f.Name())
			if err == nil && r {
				files = append(files, strings.TrimPrefix(path, project.LocalPath + "/"))
			}
		}
		return nil
	})

	if err != nil {
		log.Println("Error while getting Playbooks:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Playbooks"},
		})
		return
	}

	c.JSON(http.StatusOK, files)
}

func Teams(c *gin.Context) {
	team := c.MustGet(_CTX_PROJECT).(models.Project)

	var tms []models.Team

	var tmpTeam models.Team
	for _, v := range team.Roles {
		if v.Type == "team" {
			err := db.Teams().FindId(v.TeamID).One(&tmpTeam)
			if err != nil {
				log.Println("Error while getting Teams:", err)
				c.JSON(http.StatusInternalServerError, models.Error{
					Code:http.StatusInternalServerError,
					Messages: []string{"Error while getting Teams"},
				})
				return
			}

			err = metadata.TeamMetadata(&tmpTeam)
			if err != nil {
				log.Println("Error while setting Metatdata:", err)
				c.JSON(http.StatusInternalServerError, models.Error{
					Code:http.StatusInternalServerError,
					Messages: []string{"Error while getting Teams"},
				})
				return
			}

			tms = append(tms, tmpTeam)
		}
	}

	count := len(tms)
	pgi := util.NewPagination(c, count)
	//if page is incorrect return 404
	if pgi.HasPage() {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Invalid page " + strconv.Itoa(pgi.Page()) + ": That page contains no results."})
		return
	}
	// send response with JSON rendered data
	c.JSON(http.StatusOK, models.Response{
		Count:count,
		Next: pgi.NextPage(),
		Previous: pgi.PreviousPage(),
		Results: tms[pgi.Skip():pgi.End()],
	})
}

// TODO: not complete
func ActivityStream(c *gin.Context) {
	project := c.MustGet(_CTX_PROJECT).(models.Project)

	var activities []models.Activity
	err := db.ActivityStream().Find(bson.M{"object_id": project.ID, "type": _CTX_PROJECT}).All(&activities)

	if err != nil {
		log.Println("Error while retriving Activity data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while Activities"},
		})
	}

	count := len(activities)
	pgi := util.NewPagination(c, count)
	//if page is incorrect return 404
	if pgi.HasPage() {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Invalid page " + strconv.Itoa(pgi.Page()) + ": That page contains no results."})
		return
	}
	// send response with JSON rendered data
	c.JSON(http.StatusOK, models.Response{
		Count:count,
		Next: pgi.NextPage(),
		Previous: pgi.PreviousPage(),
		Results: activities[pgi.Skip():pgi.End()],
	})
}

// GetJobs renders the Job as JSON
func ProjectUpdates(c *gin.Context) {
	user := c.MustGet(_CTX_USER).(models.User)

	parser := util.NewQueryParser(c)

	match := bson.M{}
	match = parser.Match([]string{"status", "type", "failed"}, match)
	match = parser.Lookups([]string{"id", "name", "labels"}, match)
	log.Println(match)

	// get only project update jobs
	match["job_type"] = "update_job"

	query := db.Jobs().Find(match) // prepare the query

	// set sort value to the query based on request parameters
	if order := parser.OrderBy(); order != "" {
		query.Sort(order)
	}

	var jobs []models.Job

	// new mongodb iterator
	iter := query.Iter()
	// loop through each result and modify for our needs
	var tmpJob models.Job
	// iterate over all and only get valid objects
	for iter.Next(&tmpJob) {
		// if the user doesn't have access to credential
		// skip to next
		if !roles.JobRead(user, tmpJob) {
			continue
		}
		if err := metadata.JobMetadata(&tmpJob); err != nil {
			log.Println("Error while setting metatdata:", err)
			c.JSON(http.StatusInternalServerError, models.Error{
				Code:http.StatusInternalServerError,
				Messages: []string{"Error while getting Credentials"},
			})
			return
		}
		// good to go add to list
		jobs = append(jobs, tmpJob)
	}
	if err := iter.Close(); err != nil {
		log.Println("Error while retriving Credential data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Credential"},
		})
		return
	}

	count := len(jobs)
	pgi := util.NewPagination(c, count)
	//if page is incorrect return 404
	if pgi.HasPage() {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Invalid page " + strconv.Itoa(pgi.Page()) + ": That page contains no results."})
		return
	}
	// send response with JSON rendered data
	c.JSON(http.StatusOK, models.Response{
		Count:count,
		Next: pgi.NextPage(),
		Previous: pgi.PreviousPage(),
		Results: jobs[pgi.Skip():pgi.End()],
	})
}

func SCMUpdateInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"can_update":true})
}

func SCMUpdate(c *gin.Context) {
	// get Project from the gin.Context
	project := c.MustGet(_CTX_PROJECT).(models.Project)

	var req models.SCMUpdate
	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// accept nil request body for POST request, since all the fields are optional
		if err != io.EOF {
			// Return 400 if request has bad JSON format
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: util.GetValidationErrors(err),
			})
		}
		return
	}

	updateId, err := runners.UpdateProject(project)

	if err != nil {
		c.JSON(http.StatusMethodNotAllowed, models.Error{
			Code:http.StatusMethodNotAllowed,
			Messages: err,
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"project_update": updateId.Job.ID.Hex() })
}