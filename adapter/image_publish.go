package adapter

// ImageCDNPublisher uploads a local image file and returns an Agnes CDN URL for I2V.
type ImageCDNPublisher interface {
	PublishImageForVideo(ctx interface{}, localPath string) (string, error)
}
