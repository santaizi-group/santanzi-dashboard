package singleton

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
)

const firstNotificationDelay = time.Minute * 15

// 通知方式
var (
	NotificationList    map[string]map[uint64]*model.Notification // [NotificationMethodTag][NotificationID] -> model.Notification
	NotificationIDToTag map[uint64]string                         // [NotificationID] -> NotificationTag
	notificationsLock   sync.RWMutex
)

// InitNotification 初始化 Tag <-> ID <-> Notification 的映射
func InitNotification() {
	NotificationList = make(map[string]map[uint64]*model.Notification)
	NotificationIDToTag = make(map[uint64]string)
}

// loadNotifications 从 DB 初始化通知方式相关参数
func loadNotifications() {
	InitNotification()
	notificationsLock.Lock()
	defer notificationsLock.Unlock()

	var notifications []model.Notification
	if err := DB.Find(&notifications).Error; err != nil {
		panic(err)
	}
	for i := 0; i < len(notifications); i++ {
		// 空标签统一设置为默认通知组。
		if notifications[i].Tag == "" {
			SetDefaultNotificationTagInDB(&notifications[i])
		}
		AddNotificationToList(&notifications[i])
	}
}

// SetDefaultNotificationTagInDB 设置默认通知方式的 Tag
func SetDefaultNotificationTagInDB(n *model.Notification) {
	n.Tag = "default"
	if err := DB.Save(n).Error; err != nil {
		log.Println("SANTAIZI>> SetDefaultNotificationTagInDB 错误: ", err)
	}
}

// OnRefreshOrAddNotification 刷新通知方式相关参数
func OnRefreshOrAddNotification(n *model.Notification) {
	notificationsLock.Lock()
	defer notificationsLock.Unlock()

	var isEdit bool
	if _, ok := NotificationIDToTag[n.ID]; ok {
		isEdit = true
	}
	if !isEdit {
		AddNotificationToList(n)
	} else {
		UpdateNotificationInList(n)
	}
}

// AddNotificationToList 添加通知方式到map中
func AddNotificationToList(n *model.Notification) {
	// 当前 Tag 不存在，创建对应该 Tag 的 子 map 后再添加
	if _, ok := NotificationList[n.Tag]; !ok {
		NotificationList[n.Tag] = make(map[uint64]*model.Notification)
	}
	NotificationList[n.Tag][n.ID] = n
	NotificationIDToTag[n.ID] = n.Tag
}

// UpdateNotificationInList 在 map 中更新通知方式
func UpdateNotificationInList(n *model.Notification) {
	if n.Tag != NotificationIDToTag[n.ID] {
		// 如果 Tag 不一致，则需要先移除原有的映射关系
		delete(NotificationList[NotificationIDToTag[n.ID]], n.ID)
		delete(NotificationIDToTag, n.ID)
		// 将新的 Tag 中的通知方式添加到 map 中
		AddNotificationToList(n)
	} else {
		// 如果 Tag 一致，则直接更新
		NotificationList[n.Tag][n.ID] = n
	}
}

// OnDeleteNotification 在map中删除通知方式
func OnDeleteNotification(id uint64) {
	notificationsLock.Lock()
	defer notificationsLock.Unlock()

	delete(NotificationList[NotificationIDToTag[id]], id)
	delete(NotificationIDToTag, id)
}

func UnMuteNotification(notificationTag string, muteLabel *string) {
	fullMuteLabel := *NotificationMuteLabel.AppendNotificationTag(muteLabel, notificationTag)
	Cache.Delete(fullMuteLabel)
}

// SendNotification 向指定的通知方式组的所有通知方式发送通知
func SendNotification(notificationTag string, desc string, muteLabel *string, ext ...*model.Server) {
	if muteLabel != nil && Cache != nil {
		// 将通知方式组名称加入静音标志
		muteLabel := *NotificationMuteLabel.AppendNotificationTag(muteLabel, notificationTag)
		// 通知防骚扰策略
		var flag bool
		if cacheN, has := Cache.Get(muteLabel); has {
			nHistory := cacheN.(NotificationHistory)
			// 每次提醒都增加一倍等待时间，最后每天最多提醒一次
			if time.Now().After(nHistory.Until) {
				flag = true
				nHistory.Duration *= 2
				if nHistory.Duration > time.Hour*24 {
					nHistory.Duration = time.Hour * 24
				}
				nHistory.Until = time.Now().Add(nHistory.Duration)
				// 缓存有效期加 10 分钟
				Cache.Set(muteLabel, nHistory, nHistory.Duration+time.Minute*10)
			}
		} else {
			// 新提醒直接通知
			flag = true
			Cache.Set(muteLabel, NotificationHistory{
				Duration: firstNotificationDelay,
				Until:    time.Now().Add(firstNotificationDelay),
			}, firstNotificationDelay+time.Minute*10)
		}

		if !flag {
			if Conf.Debug {
				log.Println("SANTAIZI>> 静音的重复通知：", desc, muteLabel)
			}
			return
		}
	}
	if kind, names, ok := classifyNotification(desc); ok {
		enqueueNotificationAggregate(notificationTag, kind, names, desc)
		return
	}
	var server *model.Server
	if len(ext) > 0 {
		server = ext[0]
	}
	notificationDeliver(notificationTag, desc, server)
}

// AfterNotificationDelivered 在 Webhook 渠道投递之后调用（如 Telegram Bot 订阅转发）。
var AfterNotificationDelivered func(notificationTag, desc string, server *model.Server)

func deliverNotification(notificationTag, desc string, server *model.Server) {
	notificationsLock.RLock()
	defer notificationsLock.RUnlock()
	for _, n := range NotificationList[notificationTag] {
		log.Println("SANTAIZI>> 尝试通知", n.Name)
	}
	for _, n := range NotificationList[notificationTag] {
		ns := model.NotificationServerBundle{
			Notification: n,
			Server:       server,
			Loc:          Loc,
		}
		if err := ns.Send(desc); err != nil {
			log.Println("SANTAIZI>> 向 ", n.Name, " 发送通知失败：", err)
		} else {
			log.Println("SANTAIZI>> 向 ", n.Name, " 发送通知成功：")
		}
	}
	if AfterNotificationDelivered != nil {
		AfterNotificationDelivered(notificationTag, desc, server)
	}
}

func botMuteKey(serverID uint64) string {
	return fmt.Sprintf("bot::mute:%d", serverID)
}

func BotMuteServer(serverID uint64, duration time.Duration) {
	if Cache == nil || serverID == 0 {
		return
	}
	if duration <= 0 {
		duration = time.Hour
	}
	Cache.Set(botMuteKey(serverID), true, duration)
}

func BotUnmuteServer(serverID uint64) {
	if Cache == nil || serverID == 0 {
		return
	}
	Cache.Delete(botMuteKey(serverID))
}

func IsBotMuted(serverID uint64) bool {
	if Cache == nil || serverID == 0 {
		return false
	}
	_, ok := Cache.Get(botMuteKey(serverID))
	return ok
}

type _NotificationMuteLabel struct{}

var NotificationMuteLabel _NotificationMuteLabel

func (_NotificationMuteLabel) IPChanged(serverId uint64) *string {
	label := fmt.Sprintf("bf::ic-%d", serverId)
	return &label
}

func (_NotificationMuteLabel) ServerIncident(alertId uint64, serverId uint64) *string {
	label := fmt.Sprintf("bf::sei-%d-%d", alertId, serverId)
	return &label
}

func (_NotificationMuteLabel) ServerIncidentResolved(alertId uint64, serverId uint64) *string {
	label := fmt.Sprintf("bf::seir-%d-%d", alertId, serverId)
	return &label
}

func (_NotificationMuteLabel) AppendNotificationTag(label *string, notificationTag string) *string {
	newLabel := fmt.Sprintf("%s:%s", *label, notificationTag)
	return &newLabel
}

func (_NotificationMuteLabel) ServiceLatencyMin(serviceId uint64) *string {
	label := fmt.Sprintf("bf::sln-%d", serviceId)
	return &label
}

func (_NotificationMuteLabel) ServiceLatencyMax(serviceId uint64) *string {
	label := fmt.Sprintf("bf::slm-%d", serviceId)
	return &label
}

func (_NotificationMuteLabel) ServiceStateChanged(serviceId uint64) *string {
	label := fmt.Sprintf("bf::ssc-%d", serviceId)
	return &label
}

func (_NotificationMuteLabel) ServiceSSL(serviceId uint64, extraInfo string) *string {
	label := fmt.Sprintf("bf::sssl-%d-%s", serviceId, extraInfo)
	return &label
}

func (_NotificationMuteLabel) ServerOffline(serverId uint64) *string {
	label := fmt.Sprintf("bf::soff-%d", serverId)
	return &label
}

func (_NotificationMuteLabel) ServerRecovery(serverId uint64) *string {
	label := fmt.Sprintf("bf::srec-%d", serverId)
	return &label
}

func (_NotificationMuteLabel) ProbeDown(collectorUUID string, serverId uint64) *string {
	label := fmt.Sprintf("bf::pd-%s-%d", collectorUUID, serverId)
	return &label
}

func (_NotificationMuteLabel) ProbeUp(collectorUUID string, serverId uint64) *string {
	label := fmt.Sprintf("bf::pu-%s-%d", collectorUUID, serverId)
	return &label
}

func (_NotificationMuteLabel) ProbeLatency(collectorUUID string, serverId uint64) *string {
	label := fmt.Sprintf("bf::pl-%s-%d", collectorUUID, serverId)
	return &label
}

func (_NotificationMuteLabel) TrafficWarning(policyId uint64) *string {
	label := fmt.Sprintf("bf::tw-%d", policyId)
	return &label
}

func (_NotificationMuteLabel) TrafficExceeded(policyId uint64) *string {
	label := fmt.Sprintf("bf::te-%d", policyId)
	return &label
}
