/*
 * @Author: shgopher shgopher@gmail.com
 * @Date: 2024-09-12 12:45:24
 * @LastEditors: shgopher shgopher@gmail.com
 * @LastEditTime: 2024-09-17 20:51:59
 * @FilePath: /go3/ddd/main.go
 * @Description:
 *
 * Copyright (c) 2024 by shgopher, All Rights Reserved.
 */
package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	// 设置存储文件的目录
	storageDir := "nas_files"
	err := ensureDirectoryExists(storageDir)
	if err != nil {
		log.Fatalf("无法创建存储目录：%v", err)
	}

	// 上传文件分片
	r.POST("/upload", func(c *gin.Context) {
		fileName := c.PostForm("filename")
		chunkIndex := c.PostForm("chunkIndex")
		totalChunks := c.PostForm("totalChunks")

		// 解析分片索引
		chunkIdx, err := strconv.Atoi(chunkIndex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的分片索引"})
			return
		}

		// 获取上传的文件分片
		file, _, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无法获取文件"})
			return
		}
		defer file.Close()

		// 创建一个临时文件来存储分片
		tempFileName := fmt.Sprintf("%s_%d.tmp", fileName, chunkIdx)
		tempFilePath := filepath.Join(storageDir, tempFileName)
		out, err := os.Create(tempFilePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建临时文件"})
			return
		}
		defer out.Close()

		// 将分片写入临时文件
		_, err = io.Copy(out, file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法写入分片文件"})
			return
		}
		var strToInt = func(s string) int {
			i, _ := strconv.Atoi(s)
			return i
		}
		// 如果是最后一个分片，开始拼接所有分片
		if chunkIdx == (strToInt(totalChunks) - 1) {
			finalFilePath := filepath.Join(storageDir, fileName)
			err := mergeChunksImproved(fileName, finalFilePath, strToInt(totalChunks), storageDir)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "无法合并分片"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "文件上传成功"})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("分片 %s 上传成功", chunkIndex)})
		}
	})

	// 文件下载接口
	r.GET("/download/:filename", func(c *gin.Context) {
		filename := c.Param("filename")
		filePath := filepath.Join(storageDir, filename)

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/octet-stream")
		c.File(filePath)
	})

	// 首页，展示文件列表和上传界面
	r.GET("/", func(c *gin.Context) {
		a()
		files, err := getFileList(storageDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		htmlContent := generateHTML(files)
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(htmlContent))
	})

	// 处理在线预览文件
	r.GET("/preview/:filename", func(c *gin.Context) {
		filename := c.Param("filename")
		filePath := filepath.Join(storageDir, filename)

		contentType := getContentType(filePath)
		if contentType == "text/plain" || strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") {
			c.Header("Content-Type", contentType)
			http.ServeFile(c.Writer, c.Request, filePath)
		} else {
			c.JSON(http.StatusNotAcceptable, gin.H{"error": "不支持的文件类型进行预览"})
		}
	})

	// 添加删除文件的接口
	r.DELETE("/delete/:filename", func(c *gin.Context) {
		filename := c.Param("filename")
		filePath := filepath.Join(storageDir, filename)
		err := os.Remove(filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法删除文件"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "文件删除成功"})
	})

	// 监听所有 IPv6 和 IPv4 地址的 8080 端口
	addr := "[::]:8080"
	log.Printf("服务器启动在 %s", addr)
	err = r.Run(addr)
	if err != nil {
		log.Fatalf("无法启动服务器：%v", err)
	}
}

// 确保目录存在，如果不存在则创建
func ensureDirectoryExists(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

// 改进后的合并分片方法
func mergeChunksImproved(fileName, finalFilePath string, totalChunks int, storageDir string) error {
	out, err := os.Create(finalFilePath)
	if err != nil {
		return fmt.Errorf("无法创建最终文件: %v", err)
	}
	defer out.Close()

	for i := 0; i < totalChunks; i++ {
		chunkFilePath := filepath.Join(storageDir, fmt.Sprintf("%s_%d.tmp", fileName, i))
		chunkFile, err := os.Open(chunkFilePath)
		if err != nil {
			return fmt.Errorf("无法打开分片文件: %v", err)
		}

		// 逐块读取分片文件并写入最终文件，而不是一次性读入内存
		buffer := make([]byte, 40)
		for {
			n, err := chunkFile.Read(buffer)
			if n > 0 {
				_, err = out.Write(buffer[:n])
				if err != nil {
					return fmt.Errorf("无法合并分片文件: %v", err)
				}
			}
			if err == io.EOF {
				fmt.Println("写入完成")
				break
			}
			if err != nil {
				return fmt.Errorf("无法合并分片文件: %v", err)
			}
		}

		chunkFile.Close()

		// 删除临时分片文件
		err = os.Remove(chunkFilePath)
		if err != nil {
			return fmt.Errorf("无法删除分片文件: %v", err)
		}
	}
	return nil
}

// 获取文件列表
func getFileList(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, info.Name())
		}
		return nil
	})
	return files, err
}

// 生成 HTML 界面，添加删除按钮
func generateHTML(files []string) string {
	var buffer bytes.Buffer
	buffer.WriteString(`<!DOCTYPE html>
 <html>
 <head>
		 <title>文件上传系统</title>
		 <meta name="viewport" content="width=device-width, initial-scale=1">
 </head>
 <body>
		 <h1>文件列表</h1>
		 <ul>`)
	for _, file := range files {
		buffer.WriteString(fmt.Sprintf("<li><a href='/preview/%s'>预览 %s</a> | <a href='/download/%s'>下载 %s</a> | <button onclick='deleteFile(\"%s\")'>删除</button></li>", file, file, file, file, file))
	}
	buffer.WriteString(`</ul>
		 <h2>上传文件</h2>
		 <form id="uploadForm" enctype="multipart/form-data">
				 <input type="file" id="fileInput" name="file" multiple>
				 <button type="button" onclick="uploadFiles()">上传</button>
		 </form>
		 <p id="status"></p>
		 <script>
				 function uploadFiles() {
						 const fileInput = document.getElementById('fileInput');
						 const files = fileInput.files;
						 const chunkSize = 1 * 1024 * 1024; // 每个分片 1MB
 
						 for (let file of files) {
								 const totalChunks = Math.ceil(file.size / chunkSize);
								 for (let i = 0; i < totalChunks; i++) {
										 const chunk = file.slice(i * chunkSize, (i + 1) * chunkSize);
										 const formData = new FormData();
										 formData.append('file', chunk);
										 formData.append('filename', file.name);
										 formData.append('chunkIndex', i);
										 formData.append('totalChunks', totalChunks);
										 setTimeout(() => {}, 1000);
										 fetch('/upload', {
												 method: 'POST',
												 body: formData
										 }).then(response => response.json()).then(data => {
												 document.getElementById('status').innerText = data.message;
										 }).catch(error => {
												 console.error('上传失败:', error);
										 });
								 }
						 }
				 }
 
				 function deleteFile(filename) {
						 fetch('/delete/' + filename, {
								 method: 'DELETE'
						 }).then(response => response.json()).then(data => {
								 if (data.message === '文件删除成功') {
										 location.reload();
								 } else {
										 console.error('删除失败:', data.error);
								 }
						 }).catch(error => {
								 console.error('删除失败:', error);
						 });
				 }
		 </script>
 </body>
 </html>`)
	return buffer.String()
}

// 获取文件的内容类型
func getContentType(filePath string) string {
	ext := filepath.Ext(filePath)
	switch ext {
	case ".txt":
		return "text/plain"
	case ".png", ".jpg", ".jpeg", ".gif":
		return "image/" + ext[1:]
	case ".mp4", ".avi", ".mov", ".mkv":
		return "video/" + ext[1:]
	case ".mp3", ".wav", ".ogg":
		return "audio/" + ext[1:]
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	case ".ppt", ".pptx":
		return "application/vnd.ms-powerpoint"
	case ".zip":
		return "application/zip"
	case ".tar", ".gz", ".bz2":
		return "application/x-tar"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	default:
		// 对于未知的扩展名，返回二进制流
		return "application/octet-stream"
	}
}

func a() {
	err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".tmp" {
			err := os.Remove(path)
			if err != nil {
				return err
			} else {
				fmt.Printf("Removed file: %s\n", path)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Println("Error walking directory:", err)
	}
}
