package quark

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
)

const ucWebUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

type ucPlayResponse struct {
	Code *int `json:"code"`
	Data struct {
		VideoList []ucVideo `json:"video_list"`
	} `json:"data"`
}

type ucVideo struct {
	Resolution string `json:"resolution"`
	Accessible bool   `json:"accessable"`
	VideoInfo  struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	} `json:"video_info"`
}

var ucResolutions = []string{"low", "normal", "high", "super", "2k", "4k"}

func validateUCResolution(resolution string) error {
	if resolution == "" || resolution == "auto" || utils.SliceContains(ucResolutions, resolution) {
		return nil
	}
	return errors.New("invalid UC transcoding resolution")
}

func selectUCVideo(videos []ucVideo, resolution string) (ucVideo, error) {
	var best ucVideo
	bestRank := -1
	for _, video := range videos {
		if !video.Accessible || video.VideoInfo.URL == "" || video.VideoInfo.Format != "m3u8" {
			continue
		}
		if resolution != "" && resolution != "auto" && video.Resolution != resolution {
			continue
		}
		for rank, name := range ucResolutions {
			if name == video.Resolution && rank > bestRank {
				best, bestRank = video, rank
			}
		}
	}
	if bestRank == -1 {
		return best, errors.New("UC has no accessible HLS at the selected resolution; try auto or check the account playback permissions")
	}
	u, err := url.Parse(best.VideoInfo.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".drive.uc.cn") {
		return ucVideo{}, errors.New("UC returned an invalid HLS URL")
	}
	return best, nil
}

func (d *QuarkOrUC) ucHLSSource(ctx context.Context, fid, resolution string) (*model.HLSSource, error) {
	d.apiMu.Lock()
	defer d.apiMu.Unlock()
	// Resty's Clone shares its underlying http.Client; changing its redirect
	// policy would affect every driver. Copy that client before configuring it.
	httpClient := *base.RestyClient.GetClient()
	client := resty.NewWithClient(&httpClient).SetRedirectPolicy(resty.NoRedirectPolicy())
	res, err := client.R().SetContext(ctx).
		SetHeaders(map[string]string{"Cookie": d.Cookie, "Referer": "https://drive.uc.cn/", "User-Agent": ucWebUA}).
		SetQueryParams(map[string]string{"pr": "UCBrowser", "fr": "pc"}).
		SetBody(base.Json{"fid": fid, "resolutions": strings.Join(ucResolutions, ","), "supports": "m3u8"}).
		Post(d.conf.api + "/file/v2/play")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("UC playback request failed")
	}
	if res.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("UC playback returned HTTP %d; check the login Cookie", res.StatusCode())
	}
	var result ucPlayResponse
	if utils.Json.Unmarshal(res.Body(), &result) != nil || result.Code == nil {
		return nil, errors.New("invalid UC playback response")
	}
	if *result.Code != 0 {
		return nil, fmt.Errorf("UC playback failed (code=%d); update the Cookie if the login has expired", *result.Code)
	}
	video, err := selectUCVideo(result.Data.VideoList, resolution)
	if err != nil {
		return nil, err
	}
	videoAuth := cookie.GetCookie(res.Cookies(), "Video-Auth")
	if videoAuth == nil || videoAuth.Value == "" {
		return nil, errors.New("UC playback did not return Video-Auth")
	}
	oldCookie := d.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "__pus" || c.Name == "__puus" {
			d.Cookie = cookie.SetStr(d.Cookie, c.Name, c.Value)
		}
	}
	if oldCookie != d.Cookie && d.ID != 0 {
		op.MustSaveDriverStorage(d)
	}
	headers := http.Header{}
	headers.Set("Cookie", cookie.SetStr(d.Cookie, "Video-Auth", videoAuth.Value))
	headers.Set("User-Agent", ucWebUA)
	headers.Set("Referer", "https://drive.uc.cn/")
	return &model.HLSSource{
		URL: video.VideoInfo.URL, Header: headers,
		// Keep the selected rendition during a session so segment positions do
		// not change if account rights or available resolutions change later.
		Refresh: func(ctx context.Context) (*model.HLSSource, error) {
			return d.ucHLSSource(ctx, fid, video.Resolution)
		},
	}, nil
}
