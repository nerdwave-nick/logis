// A test shader written for logis's goldens (a5e5f876; no third-party
// code): a warm tint that fades out towards the top of the frame, and
// every fourth buffer row dimmed. The frame is premultiplied; scaling
// its colour down keeps it so.
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	vec2 uv = fragCoord / iResolution.xy;
	vec4 c = texture(iChannel0, uv);
	vec3 warm = c.rgb * mix(vec3(1.0, 0.8, 0.55), vec3(1.0), uv.y);
	float row = mod(floor(fragCoord.y), 4.0) == 0.0 ? 0.6 : 1.0;
	fragColor = vec4(warm * row, c.a);
}
