package middlewares

import (
	"context"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSTRMBlocksLegacySideEffects(t *testing.T) {
	oldMode, oldConfig := conf.STRMCommandMode, conf.Conf
	conf.STRMCommandMode, conf.Conf = true, nil
	t.Cleanup(func() { conf.STRMCommandMode, conf.Conf = oldMode, oldConfig })
	for _, path := range []string{"/api/fs/remove", "/api/fs/other", "/api/fs/multipart/complete", "/api/admin/media/strm/generate", "/api/admin/task/copy/retry", "/api/fs/seed/quick_save", "/dav/media", "/@emby/Videos/stream", "/d/private.mp4"} {
		t.Run(path, func(t *testing.T) {
			r := gin.New()
			r.Use(STRMPolicy)
			called := false
			r.Any(path, func(c *gin.Context) { called = true; c.Status(200) })
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", "even-an-admin-must-not-bypass-read-only-policy")
			r.ServeHTTP(response, request)
			if response.Code != 403 || called {
				t.Fatalf("side effect route executed: %d", response.Code)
			}
		})
	}
}

func TestSTRMAdminGuardsBeforeHandler(t *testing.T) {
	oldMode := conf.STRMCommandMode
	conf.STRMCommandMode = true
	t.Cleanup(func() { conf.STRMCommandMode = oldMode })
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), conf.UserKey, &model.User{Role: model.GENERAL}))
		c.Next()
	}, STRMAdmin)
	called := false
	r.GET("/read", func(c *gin.Context) { called = true; c.Status(200) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/read", nil))
	if called {
		t.Fatal("non-admin reached the handler before permission check")
	}
}
