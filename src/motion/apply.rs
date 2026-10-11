//! A [`Pose`] applied to a whole [`Scene`]: every position moved by its
//! offset and every colour's alpha multiplied by its opacity, so any scene
//! can open and close the same way.

use super::Pose;
use crate::render::{Paint, Rgba, Scene};

impl Pose {
    /// Moves and fades `scene` into this pose. The rest pose changes
    /// nothing. Images move but keep their own opacity (the image pipeline
    /// has no per-quad alpha yet).
    pub fn apply(self, scene: &mut Scene) {
        if self.is_rest() {
            return;
        }
        let fade = |c: Rgba| c.with_alpha(c.a * self.opacity.clamp(0.0, 1.0));
        let [dx, dy] = self.offset;
        let shift = |p: [f32; 2]| [p[0] + dx, p[1] + dy];
        let paint = |p: Paint| match p {
            Paint::Solid(c) => Paint::Solid(fade(c)),
            Paint::Linear {
                start,
                end,
                from,
                to,
            } => Paint::Linear {
                start: shift(start),
                end: shift(end),
                from: fade(from),
                to: fade(to),
            },
            Paint::Ramp(ramp) => Paint::Ramp(ramp.map(fade, shift)),
        };
        scene.clear = scene.clear.map(fade);
        for shape in &mut scene.shapes {
            shape.rect = self.place(shape.rect);
            shape.fill = paint(shape.fill);
            if let Some(border) = &mut shape.border {
                border.paint = paint(border.paint);
                if let Some(highlight) = &mut border.highlight {
                    highlight.color = fade(highlight.color);
                }
            }
            if let Some(glow) = &mut shape.glow {
                glow.color = fade(glow.color);
            }
        }
        if let Some(post) = &mut scene.post {
            post.rect = self.place(post.rect);
        }
        for image in &mut scene.images {
            image.origin = shift(image.origin);
        }
        for text in &mut scene.texts {
            text.origin = shift(text.origin);
            text.color = fade(text.color);
            for (_, color) in &mut text.spans {
                *color = fade(*color);
            }
        }
    }

    /// A rectangle (left, top, width, height) moved by this pose's offset.
    pub fn place(self, [x, y, w, h]: [f32; 4]) -> [f32; 4] {
        [x + self.offset[0], y + self.offset[1], w, h]
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::render::{Border, Glow, RoundedRect, Text};

    fn scene() -> Scene {
        let shape = RoundedRect {
            rect: [10.0, 20.0, 100.0, 40.0],
            radius: 8.0,
            fill: Paint::Linear {
                start: [0.0, 20.0],
                end: [0.0, 60.0],
                from: Rgba::new(1.0, 0.0, 0.0, 0.8),
                to: Rgba::new(0.0, 0.0, 1.0, 0.4),
            },
            border: Some(Border {
                width: 2.0,
                paint: Paint::Solid(Rgba::hex(0xd4a017)),
                highlight: None,
            }),
            glow: Some(Glow {
                radius: 12.0,
                color: Rgba::new(0.7, 0.0, 0.0, 0.5),
            }),
        };
        let mut text = Text::new("rite", [14.0, 24.0], 15.0, Rgba::hex(0xeadfc4));
        text.spans = vec![(0..2, Rgba::hex(0xf2c14e))];
        Scene {
            clear: Some(Rgba::new(0.0, 0.0, 0.0, 0.5)),
            shapes: vec![shape],
            texts: vec![text],
            ..Scene::default()
        }
    }

    #[test]
    fn the_effects_move_with_the_panel() {
        let mut s = scene();
        s.post = Some(crate::render::Post {
            rect: [10.0, 20.0, 100.0, 40.0],
            scanlines: 0.5,
            ..Default::default()
        });
        Pose {
            opacity: 0.5,
            offset: [0.0, 8.0],
        }
        .apply(&mut s);
        let post = s.post.unwrap();
        assert_eq!(post.rect, [10.0, 28.0, 100.0, 40.0]);
        assert_eq!(post.scanlines, 0.5, "the strengths stay");
    }

    #[test]
    fn the_rest_pose_changes_nothing() {
        let mut s = scene();
        Pose::REST.apply(&mut s);
        assert_eq!(s, scene());
    }

    #[test]
    fn a_pose_fades_every_colour_and_keeps_the_hue() {
        let mut s = scene();
        let pose = Pose {
            opacity: 0.5,
            offset: [0.0, 0.0],
        };
        pose.apply(&mut s);
        assert_eq!(s.clear, Some(Rgba::new(0.0, 0.0, 0.0, 0.25)));
        let shape = &s.shapes[0];
        let Paint::Linear { from, to, .. } = shape.fill else {
            panic!("still a gradient")
        };
        assert_eq!((from.a, to.a), (0.4, 0.2));
        assert_eq!(from.r, 1.0);
        assert_eq!(
            shape.border.unwrap().paint,
            Paint::Solid(Rgba::hex(0xd4a017).with_alpha(0.5))
        );
        assert_eq!(shape.glow.unwrap().color.a, 0.25);
        assert_eq!(s.texts[0].color.a, 0.5);
        assert_eq!(s.texts[0].spans[0].1.a, 0.5);
    }

    #[test]
    fn a_pose_moves_shapes_gradients_and_text_together() {
        let mut s = scene();
        let pose = Pose {
            opacity: 1.0,
            offset: [2.0, 10.0],
        };
        pose.apply(&mut s);
        assert_eq!(s.shapes[0].rect, [12.0, 30.0, 100.0, 40.0]);
        let Paint::Linear { start, end, .. } = s.shapes[0].fill else {
            panic!("still a gradient")
        };
        assert_eq!((start, end), ([2.0, 30.0], [2.0, 70.0]));
        assert_eq!(s.texts[0].origin, [16.0, 34.0]);
        assert_eq!(s.texts[0].color, scene().texts[0].color, "not faded");
    }

    #[test]
    fn opacity_is_clamped() {
        let mut s = scene();
        let pose = Pose {
            opacity: 3.0,
            offset: [0.0, 0.0],
        };
        pose.apply(&mut s);
        assert_eq!(s.texts[0].color.a, 1.0);
    }
}
