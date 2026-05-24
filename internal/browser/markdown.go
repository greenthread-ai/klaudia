package browser

import md "github.com/JohannesKaufmann/html-to-markdown"

func HTMLToMarkdown(html string) (string, error) {
	conv := md.NewConverter("", true, nil)
	return conv.ConvertString(html)
}
