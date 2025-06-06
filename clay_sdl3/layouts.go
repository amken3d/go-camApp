package main

import (
	"fmt"
	"github.com/TotallyGamerJet/clay"
	"github.com/Zyko0/go-sdl3/sdl"
	"log"
)

func createMultiCameraLayout(data *CameraAppData, renderer *sdl.Renderer) clay.RenderCommandArray {
	clay.BeginLayout()

	// Main container
	clay.UI()(clay.ElementDeclaration{
		Id:              SafeID("MainContainer"),
		BackgroundColor: clay.Color{R: 20, G: 20, B: 20, A: 255},
		Layout: clay.LayoutConfig{
			LayoutDirection: clay.TOP_TO_BOTTOM,
			Sizing: clay.Sizing{
				Width:  clay.SizingGrow(0),
				Height: clay.SizingGrow(0),
			},
			Padding:  clay.PaddingAll(16),
			ChildGap: 16,
		},
	}, func() {
		// Header
		clay.UI()(clay.ElementDeclaration{
			Id: SafeID("HeaderBar"),
			Layout: clay.LayoutConfig{
				Sizing: clay.Sizing{
					Height: clay.SizingFixed(50),
					Width:  clay.SizingGrow(0),
				},
				Padding: clay.Padding{Left: 16, Right: 16, Top: 12, Bottom: 12},
				ChildAlignment: clay.ChildAlignment{
					Y: clay.ALIGN_Y_CENTER,
				},
			},
			BackgroundColor: clay.Color{R: 100, G: 50, B: 50, A: 200},
			CornerRadius:    clay.CornerRadiusAll(1),
		}, func() {
			safeText("header-title", "Multi-Camera System", clay.TextElementConfig{
				FontId:    FontIdBody16,
				FontSize:  12,
				TextColor: clay.Color{R: 255, G: 255, B: 255, A: 255},
			})
		})

		// Main content area
		clay.UI()(clay.ElementDeclaration{
			Id: SafeID("ContentArea"),
			Layout: clay.LayoutConfig{
				LayoutDirection: clay.LEFT_TO_RIGHT,
				Sizing: clay.Sizing{
					Width:  clay.SizingGrow(0),
					Height: clay.SizingGrow(0),
				},
				ChildGap: 16,
			},
		}, func() {
			// Main camera view (left side)
			clay.UI()(clay.ElementDeclaration{
				Id: SafeID("MainCameraContainer"),
				Layout: clay.LayoutConfig{
					Sizing: clay.Sizing{
						Width:  clay.SizingPercent(0.7), // 70% of available width
						Height: clay.SizingGrow(0),
					},
					Padding: clay.PaddingAll(5),
				},
				BackgroundColor: clay.Color{R: 40, G: 40, B: 40, A: 255},
				CornerRadius:    clay.CornerRadiusAll(8),
				Border: func() clay.BorderElementConfig {
					if data.SelectedCamera < len(data.Cameras) {
						return clay.BorderElementConfig{
							Color: clay.Color{R: 0, G: 150, B: 255, A: 255},
							Width: clay.BorderAll(3),
						}
					}
					return clay.BorderElementConfig{}
				}(),
			}, func() {
				// Camera view placeholder - actual rendering happens separately
			})

			// Thumbnails panel (right side)
			clay.UI()(clay.ElementDeclaration{
				Id: SafeID("ThumbnailsPanel"),
				Layout: clay.LayoutConfig{
					LayoutDirection: clay.TOP_TO_BOTTOM,
					Sizing: clay.Sizing{
						Width:  clay.SizingFit(84, 0), // 30% of available width
						Height: clay.SizingGrow(0),
					},
					Padding:  clay.PaddingAll(8),
					ChildGap: 12,
				},
				BackgroundColor: clay.Color{R: 30, G: 30, B: 30, A: 200},
				CornerRadius:    clay.CornerRadiusAll(4),
			}, func() {
				// Thumbnails header
				if len(data.Cameras) > 0 {
					//clay.Text("Cameras", clay.TextConfig(clay.TextElementConfig{
					//	FontId:    FontIdBody16,
					//	FontSize:  20,
					//	TextColor: clay.Color{R: 255, G: 255, B: 255, A: 255},
					//}))
					safeText("thumbnail", "Cameras", clay.TextElementConfig{
						FontId:    FontIdBody16,
						FontSize:  10,
						TextColor: clay.Color{R: 255, G: 255, B: 255, A: 255},
					})
					// Camera thumbnails

					for i := range data.Cameras {
						//camera := &data.Cameras[i]
						isSelected := i == data.SelectedCamera

						// Create safe thumbnail ID
						thumbnailID := fmt.Sprintf("Thumbnail%d", i)

						clay.UI()(clay.ElementDeclaration{
							Id: SafeID(thumbnailID),
							Layout: clay.LayoutConfig{
								Sizing: clay.Sizing{
									Width:  clay.SizingGrow(80),
									Height: clay.SizingFixed(60),
								},
								Padding: clay.PaddingAll(2),
							},
							BackgroundColor: func() clay.Color {
								if isSelected {
									return clay.Color{R: 0, G: 100, B: 200, A: 255}
								} else if clay.Hovered() {
									return clay.Color{R: 60, G: 60, B: 60, A: 255}
								}
								return clay.Color{R: 50, G: 50, B: 50, A: 255}
							}(),
							CornerRadius: clay.CornerRadiusAll(4),
							Border: func() clay.BorderElementConfig {
								if isSelected {
									return clay.BorderElementConfig{
										Color: clay.Color{R: 0, G: 150, B: 255, A: 255},
										Width: clay.BorderAll(2),
									}
								}
								return clay.BorderElementConfig{}
							}(),
						}, func() {
							// Thumbnail content
							clay.UI()(clay.ElementDeclaration{
								Layout: clay.LayoutConfig{
									LayoutDirection: clay.TOP_TO_BOTTOM,
									ChildGap:        4,
									Padding:         clay.PaddingAll(4),
								},
							}, func() {})
						})
						safeText("thumbnail", fmt.Sprintf("Cam %x", i), clay.TextElementConfig{
							FontId:    FontIdBody16,
							FontSize:  8,
							TextColor: clay.Color{R: 255, G: 255, B: 255, A: 255},
						})
					}
				} else {
					safeText("no_cam", "No cameras found", clay.TextElementConfig{
						FontId:    FontIdBody16,
						FontSize:  16,
						TextColor: clay.Color{R: 255, G: 100, B: 100, A: 255},
					})
				}
			})
		})

		// Status bar
		clay.UI()(clay.ElementDeclaration{
			Id: SafeID("StatusBar"),
			Layout: clay.LayoutConfig{
				Sizing: clay.Sizing{
					Height: clay.SizingFixed(40),
					Width:  clay.SizingGrow(0),
				},
				Padding: clay.Padding{Left: 16, Right: 16, Top: 8, Bottom: 8},
				ChildAlignment: clay.ChildAlignment{
					Y: clay.ALIGN_Y_CENTER,
				},
			},
			BackgroundColor: clay.Color{R: 40, G: 40, B: 40, A: 200},
			CornerRadius:    clay.CornerRadiusAll(5),
		}, func() {
			statusText := sanitizeText(data.StatusText)
			if len(data.Cameras) > 0 && data.SelectedCamera < len(data.Cameras) {
				selectedCamera := &data.Cameras[data.SelectedCamera]
				// Clean camera name for display
				cameraName := sanitizeText(selectedCamera.Info.Name)
				if cameraName == "" || cameraName == "Unknown" {
					cameraName = fmt.Sprintf("Camera %d", data.SelectedCamera+1)
				}
				statusText = fmt.Sprintf("%s | Selected: %s | Use arrows or numbers",
					sanitizeText(data.StatusText), cameraName)
			}

			//clay.Text(statusText, clay.TextConfig(clay.TextElementConfig{
			//	FontId:    FontIdBody16,
			//	FontSize:  14,
			//	TextColor: data.StatusColor,
			//}))
			safeText("stat", statusText, clay.TextElementConfig{
				FontId:    FontIdBody16,
				FontSize:  14,
				TextColor: data.StatusColor,
			})
		})
	})

	renderCommands := clay.EndLayout()
	return renderCommands
}

func renderMainCameraView(appData *CameraAppData) {
	// Get the main camera container position and size
	mainCameraElement := clay.GetElementData(SafeID("MainCameraContainer"))
	if !mainCameraElement.Found {
		return
	}

	bbox := mainCameraElement.BoundingBox
	cameraRect := sdl.FRect{
		X: bbox.X + 5,
		Y: bbox.Y + 5,
		W: bbox.Width - 10,
		H: bbox.Height - 10,
	}

	// Render the selected camera or placeholder
	if appData.SelectedCamera < len(appData.Cameras) {
		camera := &appData.Cameras[appData.SelectedCamera]
		camera.FrameMutex.RLock()
		if camera.Texture != nil && camera.Active {
			err := appData.Renderer.RenderTexture(camera.Texture, nil, &cameraRect)
			if err != nil {
				log.Printf("Error rendering camera texture: %v", err)
				return
			}
		} else if appData.PlaceholderTexture != nil {
			err := appData.Renderer.RenderTexture(appData.PlaceholderTexture, nil, &cameraRect)
			if err != nil {
				log.Printf("Error rendering placeholder texture: %v", err)
				return
			}
		}
		camera.FrameMutex.RUnlock()
	} else if appData.PlaceholderTexture != nil {
		err := appData.Renderer.RenderTexture(appData.PlaceholderTexture, nil, &cameraRect)
		if err != nil {
			log.Printf("Error rendering placeholder texture: %v", err)
			return
		}
	}
}

func renderThumbnailViews(appData *CameraAppData) {
	for i := range appData.Cameras {
		thumbnailID := fmt.Sprintf("Thumbnail%d", i)
		thumbnailElement := clay.GetElementData(SafeID(thumbnailID))
		if !thumbnailElement.Found {
			continue
		}

		bbox := thumbnailElement.BoundingBox
		thumbnailRect := sdl.FRect{
			X: bbox.X + 2,
			Y: bbox.Y + 2,
			W: bbox.Width - 4,
			H: bbox.Height - 4,
		}

		camera := &appData.Cameras[i]
		camera.FrameMutex.RLock()
		if camera.ThumbnailTexture != nil && camera.Active {
			err := appData.Renderer.RenderTexture(camera.ThumbnailTexture, nil, &thumbnailRect)
			if err != nil {
				log.Printf("Error rendering camera thumbnail: %v", err)
				return
			}
		} else if appData.PlaceholderTexture != nil {
			err := appData.Renderer.RenderTexture(appData.PlaceholderTexture, nil, &thumbnailRect)
			if err != nil {
				log.Printf("Error rendering placeholder texture: %v", err)
				return
			}
		}
		camera.FrameMutex.RUnlock()
	}
}
