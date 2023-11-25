ALTER TABLE `isupipe`.`livecomments` ADD INDEX `livecomments_livestream_id` (`livestream_id`);
ALTER TABLE `isupipe`.`livestreams` ADD INDEX `livestreams_user_id` (`user_id`);
ALTER TABLE `isupipe`.`reactions` ADD INDEX `reactions_livestream_id` (`livestream_id`);
ALTER TABLE `isupipe`.`livestream_tags` ADD INDEX `livestream_tags_livestream_id` (`livestream_id`);
ALTER TABLE `isupipe`.`icons` ADD INDEX `icons_user_id` (`user_id`);
ALTER TABLE `isupipe`.`themes` ADD INDEX `themes_user_id` (`user_id`);
