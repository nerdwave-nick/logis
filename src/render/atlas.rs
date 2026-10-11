//! The image atlas: one GPU texture holding every image the renderer draws
//! (icons now, theme images later), each uploaded once. Pixels are
//! premultiplied RGBA8 with sRGB-encoded channels, exactly as the SDF
//! pipeline takes its colours. When full, the texture doubles on the GPU and
//! every [`ImageId`] stays valid.

use std::fmt;

use super::Gpu;
use super::offscreen::Image;
use super::packer::ShelfPacker;

/// The texture's side when the renderer starts.
const INITIAL_SIDE: u32 = 512;
/// The largest side the atlas grows to (or the device's limit if smaller).
const MAX_SIDE: u32 = 8192;
/// The atlas texture's format: values are stored as given, no sRGB decoding.
pub(crate) const ATLAS_FORMAT: wgpu::TextureFormat = wgpu::TextureFormat::Rgba8Unorm;

/// An image in the atlas.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct ImageId(u32);

/// Why an image did not enter the atlas.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AtlasError {
    /// The image has no pixels, or fewer bytes than its size needs.
    Malformed {
        /// Its width.
        width: u32,
        /// Its height.
        height: u32,
    },
    /// A side of the image exceeds the atlas's largest side.
    TooLarge {
        /// Its width.
        width: u32,
        /// Its height.
        height: u32,
        /// The largest atlas side.
        max: u32,
    },
    /// The atlas is at its largest and has no room left.
    Full {
        /// The atlas side in pixels.
        side: u32,
    },
}

impl fmt::Display for AtlasError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Malformed { width, height } => write!(
                f,
                "a malformed {width}x{height} image was refused by the image atlas"
            ),
            Self::TooLarge { width, height, max } => write!(
                f,
                "a {width}x{height} image cannot fit the image atlas's largest side of {max}"
            ),
            Self::Full { side } => write!(
                f,
                "the {side}x{side} image atlas is full; no reliquary remains for another image"
            ),
        }
    }
}

impl std::error::Error for AtlasError {}

/// The GPU texture and where each image sits in it.
pub struct ImageAtlas {
    texture: wgpu::Texture,
    view: wgpu::TextureView,
    packer: ShelfPacker,
    /// x, y, width, height in texels, by [`ImageId`].
    rects: Vec<[u32; 4]>,
    max_side: u32,
    /// Bumped whenever the texture is replaced, so bind groups can follow.
    generation: u64,
}

impl ImageAtlas {
    pub(crate) fn new(gpu: &Gpu) -> Self {
        let max_side = MAX_SIDE.min(gpu.device.limits().max_texture_dimension_2d);
        let side = INITIAL_SIDE.min(max_side);
        let texture = create_texture(gpu, side);
        Self {
            view: texture.create_view(&wgpu::TextureViewDescriptor::default()),
            texture,
            packer: ShelfPacker::new(side, side),
            rects: Vec::new(),
            max_side,
            generation: 0,
        }
    }

    /// Uploads `image` (premultiplied RGBA8) and returns its handle. Growing
    /// and uploading are queued on the GPU; nothing waits.
    pub fn insert(&mut self, gpu: &Gpu, image: &Image) -> Result<ImageId, AtlasError> {
        let (width, height) = (image.width, image.height);
        let bytes = u64::from(width) * u64::from(height) * 4;
        if width == 0 || height == 0 || (image.pixels.len() as u64) < bytes {
            return Err(AtlasError::Malformed { width, height });
        }
        if width.max(height) >= self.max_side {
            let max = self.max_side;
            return Err(AtlasError::TooLarge { width, height, max });
        }
        let (x, y) = loop {
            if let Some(at) = self.packer.allocate(width, height) {
                break at;
            }
            let side = self.packer.size().0;
            if side.saturating_mul(2) > self.max_side {
                return Err(AtlasError::Full { side });
            }
            self.grow(gpu);
        };
        gpu.queue.write_texture(
            wgpu::TexelCopyTextureInfo {
                texture: &self.texture,
                mip_level: 0,
                origin: wgpu::Origin3d { x, y, z: 0 },
                aspect: wgpu::TextureAspect::All,
            },
            &image.pixels[..bytes as usize],
            wgpu::TexelCopyBufferLayout {
                offset: 0,
                bytes_per_row: Some(width * 4),
                rows_per_image: Some(height),
            },
            extent(width, height),
        );
        let id = ImageId(u32::try_from(self.rects.len()).expect("fewer than 2^32 images"));
        self.rects.push([x, y, width, height]);
        Ok(id)
    }

    /// The pixel size of `image`, or `None` for a handle from another atlas.
    pub fn size_of(&self, image: ImageId) -> Option<(u32, u32)> {
        self.rect(image).map(|[_, _, w, h]| (w, h))
    }

    /// How many images the atlas holds.
    pub fn len(&self) -> usize {
        self.rects.len()
    }

    /// Whether the atlas holds no image.
    pub fn is_empty(&self) -> bool {
        self.rects.is_empty()
    }

    /// The texture's side in pixels.
    pub fn side(&self) -> u32 {
        self.packer.size().0
    }

    pub(crate) fn rect(&self, image: ImageId) -> Option<[u32; 4]> {
        self.rects.get(image.0 as usize).copied()
    }

    pub(crate) fn view(&self) -> &wgpu::TextureView {
        &self.view
    }

    pub(crate) fn generation(&self) -> u64 {
        self.generation
    }

    /// Doubles the texture and copies the old one into its top-left corner
    /// on the GPU, so every placed image keeps its texels.
    fn grow(&mut self, gpu: &Gpu) {
        let old = self.packer.size().0;
        self.packer.grow();
        let texture = create_texture(gpu, self.packer.size().0);
        let mut encoder = gpu
            .device
            .create_command_encoder(&wgpu::CommandEncoderDescriptor {
                label: Some("logis atlas growth"),
            });
        encoder.copy_texture_to_texture(
            self.texture.as_image_copy(),
            texture.as_image_copy(),
            extent(old, old),
        );
        gpu.queue.submit([encoder.finish()]);
        self.view = texture.create_view(&wgpu::TextureViewDescriptor::default());
        self.texture = texture;
        self.generation += 1;
    }
}

fn create_texture(gpu: &Gpu, side: u32) -> wgpu::Texture {
    gpu.device.create_texture(&wgpu::TextureDescriptor {
        label: Some("logis image atlas"),
        size: extent(side, side),
        mip_level_count: 1,
        sample_count: 1,
        dimension: wgpu::TextureDimension::D2,
        format: ATLAS_FORMAT,
        usage: wgpu::TextureUsages::TEXTURE_BINDING
            | wgpu::TextureUsages::COPY_DST
            | wgpu::TextureUsages::COPY_SRC,
        view_formats: &[],
    })
}

fn extent(width: u32, height: u32) -> wgpu::Extent3d {
    wgpu::Extent3d {
        width,
        height,
        depth_or_array_layers: 1,
    }
}
