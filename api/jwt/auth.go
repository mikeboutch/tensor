package jwt

import (
	"time"
	"github.com/gin-gonic/gin"
	"gopkg.in/appleboy/gin-jwt.v2"
	"bitbucket.pearson.com/apseng/tensor/models"
	"bitbucket.pearson.com/apseng/tensor/db"
	"gopkg.in/mgo.v2/bson"
	"strings"
	"net/mail"
	"bitbucket.pearson.com/apseng/tensor/util"
	"golang.org/x/crypto/bcrypt"
)

var HeaderAuthMiddleware *jwt.GinJWTMiddleware

func init() {
	HeaderAuthMiddleware = &jwt.GinJWTMiddleware{
		Realm:      "api",
		Key:        []byte(util.Config.CookieHash),
		Timeout:    time.Hour,
		MaxRefresh: time.Hour,
		Authenticator: func(loginid string, password string, c *gin.Context) (string, bool) {

			// Lowercase email or username
			login := strings.ToLower(loginid)

			var q bson.M

			if _, err := mail.ParseAddress(login); err == nil {
				q = bson.M{"email": login}

			} else {
				q = bson.M{"username": login}
			}

			var user models.User

			col := db.C(models.DBC_USERS)

			if err := col.Find(q).One(&user); err != nil {
				return "", false
			}

			if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
				return "", false
			}

			return user.ID.Hex(), true

		},
		Authorizator: func(userID string, c *gin.Context) bool {
			var user models.User
			col := db.C(models.DBC_USERS)
			if err := col.FindId(bson.ObjectIdHex(userID)).One(&user); err != nil {
				return false
			}
			return true
		},
		Unauthorized: func(c *gin.Context, code int, message string) {
			c.JSON(code, gin.H{
				"code":    code,
				"message": message,
			})
		},
		// TokenLookup is a string in the form of "<source>:<name>" that is used
		// to extract token from the request.
		// Optional. Default value "header:Authorization".
		// Possible values:
		// - "header:<name>"
		// - "query:<name>"
		// - "cookie:<name>"
		TokenLookup: "header:Authorization",
		// TokenLookup: "query:token",
		// TokenLookup: "cookie:token",
	}
}