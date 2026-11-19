package handles

import (
	"github.com/OpenListTeam/OpenList/v4/internal/cloudhls"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

// CloudHLS serves only resources already discovered in an authenticated
// provider playlist. The opaque token is scoped to a single playback session.
func CloudHLS(c *gin.Context) {
	token := c.Param("session")
	cloudhls.Default().Serve(c.Writer, c.Request, token, c.Param("resource"), common.GetApiUrlFromRequest(c.Request)+"/hls/"+token)
}
