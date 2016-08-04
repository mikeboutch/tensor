package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gamunu/tensor/api"
	"github.com/gamunu/tensor/api/addhoctasks"
	"github.com/gamunu/tensor/api/sockets"
	"github.com/gamunu/tensor/api/tasks"
	database "github.com/gamunu/tensor/db"
	"github.com/gamunu/tensor/models"
	"github.com/gamunu/tensor/util"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/mgo.v2/bson"
	"log"
)

func main() {
	if util.InteractiveSetup {
		os.Exit(doSetup())
	}

	fmt.Printf("Tensor %v\n", util.Version)
	fmt.Printf("Port %v\n", util.Config.Port)
	fmt.Printf("MongoDB %v@%v %v\n", util.Config.MongoDB.Username, util.Config.MongoDB.Hosts, util.Config.MongoDB.DbName)
	fmt.Printf("Tmp Path (projects home) %v\n", util.Config.TmpPath)

	if err := database.Connect(); err != nil {
		panic(err)
	}

	defer func() {
		database.MongoDb.Session.Close()
	}()

	go sockets.StartWS()

	//Define custom validator
	binding.Validator = &util.SpaceValidator{}

	r := gin.New()
	r.Use(gin.Recovery(), recovery, gin.Logger())

	api.Route(r)

	go tasks.StartRunner()
	go addhoctasks.StartRunner()

	r.Run(util.Config.Port)

}

func recovery(c *gin.Context) {

	//report to bug nofiy system
	c.Next()
}

func doSetup() int {
	fmt.Println("Checking database connectivity.. Please be patient.")

	if err := database.Connect(); err != nil {
		log.Fatal("\n Cannot connect to database!\n" + err.Error())
		os.Exit(1)
	}

	stdin := bufio.NewReader(os.Stdin)

	var user models.User
	user.Username = readNewline("\n\n > Username: ", stdin)
	user.Username = strings.ToLower(user.Username)
	user.Email = readNewline(" > Email: ", stdin)
	user.Email = strings.ToLower(user.Email)

	var existingUser models.User

	userc := database.MongoDb.C("users")
	err := userc.Find(bson.M{"email": user.Email, "username": user.Username}).One(&existingUser)

	if err == nil {
		// user already exists
		fmt.Printf("\n Welcome back, %v! (a user with this username/email is already set up..)\n\n", existingUser.Name)
	} else {
		user.Name = readNewline(" > Your name: ", stdin)
		user.Password = readNewline(" > Password: ", stdin)
		pwdHash, _ := bcrypt.GenerateFromPassword([]byte(user.Password), 11)

		newUser := models.User{
			ID:       bson.NewObjectId(),
			Name:     user.Name,
			Username: user.Username,
			Email:    user.Email,
			Password: string(pwdHash),
			Created:  time.Now(),
		}

		if err := newUser.Insert(); err != nil {
			fmt.Printf(" Inserting user failed. If you already have a user, you can disregard this error.\n %v\n", err.Error())
			os.Exit(1)
		}

		fmt.Printf("\n You are all setup %v!\n", user.Name)
	}
	fmt.Printf(" You can login with %v or %v.\n", user.Email, user.Username)

	return 0
}

func readNewline(pre string, stdin *bufio.Reader) string {
	fmt.Print(pre)

	str, _ := stdin.ReadString('\n')
	str = strings.Replace(str, "\n", "", -1)

	return str
}