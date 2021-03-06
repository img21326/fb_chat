package ws

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	pubmessage "github.com/img21326/fb_chat/structure/pub_message"

	errStruct "github.com/img21326/fb_chat/structure/error"

	"github.com/img21326/fb_chat/usecase/auth"
	"github.com/img21326/fb_chat/usecase/ws"
	"github.com/img21326/fb_chat/ws/client"
	"gorm.io/gorm"
)

type WebsocketController struct {
	WSUpgrader websocket.Upgrader

	AuthUsecase auth.AuthUsecaseInterFace
	WsUsecase   ws.WebsocketUsecaseInterface

	ClientQueueChan chan *client.Client
	PubMessageChan  chan *pubmessage.PublishMessage
}

func NewWebsocketController(e gin.IRoutes,
	wsUsecase ws.WebsocketUsecaseInterface,
	authUsecase auth.AuthUsecaseInterFace,

	pubMessageChan chan *pubmessage.PublishMessage,
	clientQueueChan chan *client.Client,
) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	controller := WebsocketController{
		WSUpgrader:  upgrader,
		WsUsecase:   wsUsecase,
		AuthUsecase: authUsecase,

		ClientQueueChan: clientQueueChan,
		PubMessageChan:  pubMessageChan,
	}

	e.GET("/ws", controller.WS)
}

func (c *WebsocketController) WS(ctx *gin.Context) {
	conn, err := c.WSUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		log.Printf("create ws connection error: %v", err)
		return
	}
	contextBackground, cancel := context.WithCancel(context.Background())
	client := client.Client{
		Conn:           conn,
		Send:           make(chan []byte, 256),
		PubMessageChan: c.PubMessageChan,
		Ctx:            contextBackground,
		CtxCancel:      cancel,
	}
	go client.WritePump()

	token, ok := ctx.GetQuery("token")
	if !ok {
		log.Printf("connection not set token")
		client.Send <- []byte("{'error': 'NotSetToken'}")
		cancel()
		return
	}
	user, err := c.AuthUsecase.VerifyToken(token)
	if err != nil {
		log.Printf("verify token error: %v", err)
		client.Send <- []byte(fmt.Sprintf("{'error': '%v'}", err))
		cancel()
		return
	}
	client.User = *user

	log.Printf("new ws connection: %v", user.UUID)
	room, err := c.WsUsecase.FindRoomByUserId(ctx, user.ID)
	if err != nil && err != gorm.ErrRecordNotFound && err != errStruct.RoomIsClose {
		log.Printf("find room error: %v", err)
		client.Send <- []byte("{'error': 'FindRoomError'}")
		cancel()
		return
	}
	log.Printf("find room %v", room)
	c.WsUsecase.Register(ctx, &client)

	if room != nil {
		log.Printf("new ws connection: %v in room %v", user.UUID, room.ID)
		client.RoomId = room.UUID
		if room.UserId1 == client.User.ID {
			client.PairId = room.UserId2
		} else {
			client.PairId = room.UserId1
		}
		client.Send <- []byte("{'type': 'InRoom'}")
	} else {
		log.Printf("new ws connection: %v with new pairing", user.UUID)
		want, ok := ctx.GetQuery("want")
		if !ok {
			log.Print("not set want params err")
			client.Send <- []byte("{'error': 'NotSetWantParams'}")
			cancel()
			return
		}
		client.WantToFind = want
		c.ClientQueueChan <- &client
		client.Send <- []byte("{'type': 'Paring'}")
	}
	go client.ReadPump()
}
