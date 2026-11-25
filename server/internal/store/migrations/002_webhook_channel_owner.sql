-- webhook_channels.owner_id 投递归属：
--   NULL  = 空间级渠道（群 / 共享），订阅该事件即推送，文案用第三人称具名；
--   非空  = 该用户的私人渠道，仅当此人属于本次通知收件人时推送，文案用第二人称。
ALTER TABLE webhook_channels ADD COLUMN owner_id INTEGER REFERENCES users(id);
