package tilerenderer

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"mapserver/coords"
	"mapserver/db"
	"mapserver/layer"
	"mapserver/mapblockrenderer"
	"mapserver/tiledb"
	"time"

	"github.com/disintegration/imaging"
	"github.com/sirupsen/logrus"
)

type TileRenderer struct {
	mapblockrenderer *mapblockrenderer.MapBlockRenderer
	layers           []layer.Layer
	tdb              tiledb.DBAccessor
	dba              db.DBAccessor
}

func NewTileRenderer(mapblockrenderer *mapblockrenderer.MapBlockRenderer,
	tdb tiledb.DBAccessor,
	dba db.DBAccessor,
	layers []layer.Layer) *TileRenderer {

	return &TileRenderer{
		mapblockrenderer: mapblockrenderer,
		layers:           layers,
		tdb:              tdb,
		dba:              dba,
	}
}

const (
	IMG_SIZE = 256
)

func (tr *TileRenderer) Render(tc *coords.TileCoords) ([]byte, error) {

	//Check cache
	tile, err := tr.tdb.GetTile(tc)
	if err != nil {
		return nil, err
	}

	if tile == nil {
		//No tile in db
		img, err := tr.RenderImage(tc, false)

		if err != nil {
			return nil, err
		}

		if img == nil {
			//empty tile
			return nil, nil
		}

		buf := new(bytes.Buffer)
		png.Encode(buf, img)

		return buf.Bytes(), nil
	}

	return tile.Data, nil
}

func (tr *TileRenderer) RenderImage(tc *coords.TileCoords, cachedOnly bool) (*image.NRGBA, error) {

	cachedtile, err := tr.tdb.GetTile(tc)
	if err != nil {
		return nil, err
	}

	if cachedtile != nil {
		reader := bytes.NewReader(cachedtile.Data)
		cachedimg, err := png.Decode(reader)
		if err != nil {
			return nil, err
		}

		rect := image.Rectangle{
			image.Point{0, 0},
			image.Point{IMG_SIZE, IMG_SIZE},
		}

		img := image.NewNRGBA(rect)
		draw.Draw(img, rect, cachedimg, image.ZP, draw.Src)

		return img, nil
	}

	if cachedOnly {
		return nil, nil
	}

	log.WithFields(logrus.Fields{"x": tc.X, "y": tc.Y, "zoom": tc.Zoom}).Debug("RenderImage")

	var layer *layer.Layer

	for _, l := range tr.layers {
		if l.Id == tc.LayerId {
			layer = &l
		}
	}

	if layer == nil {
		return nil, errors.New("No layer found")
	}

	if tc.Zoom > 13 || tc.Zoom < 1 {
		return nil, errors.New("Invalid zoom")
	}

	if tc.Zoom == 13 {
		//max zoomed in on mapblock level
		mbr := coords.GetMapBlockRangeFromTile(tc, 0)
		mbr.Pos1.Y = layer.From
		mbr.Pos2.Y = layer.To

		return tr.mapblockrenderer.Render(mbr.Pos1, mbr.Pos2)
	}

	//zoom 1-12
	quads := tc.GetZoomedQuadrantsFromTile()

	recursiveCachedOnly := tc.Zoom < 12

	upperLeft, err := tr.RenderImage(quads.UpperLeft, recursiveCachedOnly)
	if err != nil {
		return nil, err
	}

	upperRight, err := tr.RenderImage(quads.UpperRight, recursiveCachedOnly)
	if err != nil {
		return nil, err
	}

	lowerLeft, err := tr.RenderImage(quads.LowerLeft, recursiveCachedOnly)
	if err != nil {
		return nil, err
	}

	lowerRight, err := tr.RenderImage(quads.LowerRight, recursiveCachedOnly)
	if err != nil {
		return nil, err
	}

	img := image.NewNRGBA(
		image.Rectangle{
			image.Point{0, 0},
			image.Point{IMG_SIZE, IMG_SIZE},
		},
	)

	rect := image.Rect(0, 0, 128, 128)
	if upperLeft != nil {
		resizedImg := imaging.Resize(upperLeft, 128, 128, imaging.Lanczos)
		draw.Draw(img, rect, resizedImg, image.ZP, draw.Src)
	}

	rect = image.Rect(128, 0, 256, 128)
	if upperRight != nil {
		resizedImg := imaging.Resize(upperRight, 128, 128, imaging.Lanczos)
		draw.Draw(img, rect, resizedImg, image.ZP, draw.Src)
	}

	rect = image.Rect(0, 128, 128, 256)
	if lowerLeft != nil {
		resizedImg := imaging.Resize(lowerLeft, 128, 128, imaging.Lanczos)
		draw.Draw(img, rect, resizedImg, image.ZP, draw.Src)
	}

	rect = image.Rect(128, 128, 256, 256)
	if lowerRight != nil {
		resizedImg := imaging.Resize(lowerRight, 128, 128, imaging.Lanczos)
		draw.Draw(img, rect, resizedImg, image.ZP, draw.Src)
	}

	buf := new(bytes.Buffer)
	if img != nil {
		png.Encode(buf, img)
	}

	tile := tiledb.Tile{Pos: tc, Data: buf.Bytes(), Mtime: time.Now().Unix()}
	tr.tdb.SetTile(&tile)

	return img, nil
}