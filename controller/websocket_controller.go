package controller

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/img21326/fb_chat/helper"
	MessageHub "github.com/img21326/fb_chat/hub/message"
	PairHub "github.com/img21326/fb_chat/hub/pair"
	PubSubHub "github.com/img21326/fb_chat/hub/pubsub"
	pubmessage "github.com/img21326/fb_chat/structure/pub_message"
	"github.com/img21326/fb_chat/structure/user"
	"github.com/img21326/fb_chat/usecase/message"
	"github.com/img21326/fb_chat/usecase/pair"
	"github.com/img21326/fb_chat/usecase/pubsub"
	"github.com/img21326/fb_chat/usecase/ws"
	"github.com/img21326/fb_chat/ws/client"
	"gorm.io/gorm"
)

type WebsocketController struct {
	WSUpgrader     websocket.Upgrader
	WsUsecase      ws.WebsocketUsecaseInterface
	PubsubUscase   pubsub.SubMessageUsecaseInterface
	PairUsecase    pair.PairUsecaseInterface
	MessageUsecase message.MessageUsecaseInterface

	InsertClientToQueueChan chan *client.Client
	PubMessageChan          chan *pubmessage.PublishMessage
	SubMessageChan          chan *pubmessage.PublishMessage
}

func NewWebsocketController(e gin.IRoutes,
	wsUsecase ws.WebsocketUsecaseInterface,
	pubsubUsecase pubsub.SubMessageUsecaseInterface,
	pairUsecase pair.PairUsecaseInterface,
	messageUsecase message.MessageUsecaseInterface,
) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	controller := WebsocketController{
		WSUpgrader:     upgrader,
		WsUsecase:      wsUsecase,
		PubsubUscase:   pubsubUsecase,
		PairUsecase:    pairUsecase,
		MessageUsecase: messageUsecase,

		InsertClientToQueueChan: make(chan *client.Client, 1024),
		PubMessageChan:          make(chan *pubmessage.PublishMessage, 4096),
		SubMessageChan:          make(chan *pubmessage.PublishMessage, 4096),
	}

	ctx := context.Background()

	subHub := PubSubHub.NewSubHub(pubsubUsecase)
	pubHub := PubSubHub.NewPubHub(pubsubUsecase)
	messageHub := MessageHub.NewMessageHub(messageUsecase, wsUsecase, controller.SubMessageChan)
	pairHub := PairHub.NewPairHub(pairUsecase, controller.PubMessageChan, controller.InsertClientToQueueChan)

	go subHub.Run(ctx, "message", controller.SubMessageChan)
	go pubHub.Run(ctx, "message", controller.PubMessageChan)
	go messageHub.Run(ctx)
	go pairHub.Run(ctx)

	e.GET("/ws", controller.WS)
}

func (c *WebsocketController) WS(ctx *gin.Context) {
	token := ctx.Query("token")
	id, _ := strconv.Atoi(token)
	// user, err := c.AuthUsecase.VerifyToken(token)
	// if err != nil {
	// 	log.Printf("token error: %v", err)
	// 	return
	// }

	m := gorm.Model{
		ID: uint(id),
	}
	user := &user.User{
		Model:  m,
		FbID:   helper.RandString(16),
		Name:   helper.RandString(5),
		Gender: "male",
	}
	log.Printf("new ws connection: %v", user.Name)
	room, err := c.WsUsecase.FindRoomByUserId(ctx, user.ID)
	if err != nil && err != gorm.ErrRecordNotFound && err.Error() != "RoomIsClosed" {
		log.Printf("find room error: %v", err)
		return
	}
	conn, err := c.WSUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		log.Printf("ws error: %v", err)
		return
	}
	contextBackground, cancel := context.WithCancel(context.Background())
	client := client.Client{
		Conn:           conn,
		Send:           make(chan []byte, 256),
		PubMessageChan: c.PubMessageChan,
		User:           *user,
		Ctx:            contextBackground,
		CtxCancel:      cancel,
	}
	c.WsUsecase.Register(ctx, &client)
	if room != nil {
		log.Printf("new ws connection: %v in room %v", user.Name, room.ID)
		client.RoomId = room.ID
		if room.UserId1 == client.User.ID {
			client.PairId = room.UserId2
		} else {
			client.PairId = room.UserId1
		}
		client.Send <- []byte("{'type': 'inRoom'}")
	} else {
		log.Printf("new ws connection: %v with new pairing", user.Name)
		want, ok := ctx.GetQuery("want")
		if !ok {
			log.Printf("ws not set want param")
			client.Conn.Close()
			return
		}
		client.WantToFind = want
		c.PairUsecase.AddToQueue(ctx, &client)
		client.Send <- []byte("{'type': 'paring'}")
	}

	go client.ReadPump()
	go client.WritePump()

}
