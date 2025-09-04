from PIL import Image

# Imagen azul
img_blue = Image.new("RGB", (320, 240), (0, 0, 255))
img_blue.save("blue.png")

# Imagen roja
img_red = Image.new("RGB", (320, 240), (255, 0, 0))
img_red.save("red.png")