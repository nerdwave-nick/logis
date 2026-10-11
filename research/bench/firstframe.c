// firstframe: how long from process launch until a layer-shell surface's
// first frame is *presented*, for three ways of producing pixels:
//   shm  — CPU-filled wl_shm buffer
//   egl  — EGL + GLES2 glClear + eglSwapBuffers
//   vk   — Vulkan instance/device/swapchain, render-pass clear, present
// Times are CLOCK_MONOTONIC ms relative to $T0_NS (set by the launcher just
// before exec), so they include exec and dynamic linking. Research tooling.
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <sys/mman.h>
#include <wayland-client.h>
#include "wlr-layer-shell-unstable-v1-client.h"
#include "presentation-time-client.h"
#ifdef WITH_EGL
#include <wayland-egl.h>
#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <GLES2/gl2.h>
#endif
#ifdef WITH_VK
#define VK_USE_PLATFORM_WAYLAND_KHR
#include <vulkan/vulkan.h>
#endif

static long long t0;
static long long now_ns(void) { struct timespec ts; clock_gettime(CLOCK_MONOTONIC, &ts); return ts.tv_sec * 1000000000LL + ts.tv_nsec; }
static void mark(const char *what) { fprintf(stderr, "%-24s %8.2f ms\n", what, (now_ns() - t0) / 1e6); }

static struct wl_compositor *compositor;
static struct wl_shm *shm;
static struct zwlr_layer_shell_v1 *layer_shell;
static struct wp_presentation *presentation;
static int configured, presented, W = 832, H = 351;
static uint32_t cfg_serial;

static void reg_global(void *d, struct wl_registry *r, uint32_t name, const char *iface, uint32_t v) {
    if (!strcmp(iface, wl_compositor_interface.name)) compositor = wl_registry_bind(r, name, &wl_compositor_interface, 4);
    else if (!strcmp(iface, wl_shm_interface.name)) shm = wl_registry_bind(r, name, &wl_shm_interface, 1);
    else if (!strcmp(iface, zwlr_layer_shell_v1_interface.name)) layer_shell = wl_registry_bind(r, name, &zwlr_layer_shell_v1_interface, 4);
    else if (!strcmp(iface, wp_presentation_interface.name)) presentation = wl_registry_bind(r, name, &wp_presentation_interface, 1);
}
static void reg_remove(void *d, struct wl_registry *r, uint32_t n) {}
static const struct wl_registry_listener reg_l = {reg_global, reg_remove};

static void ls_configure(void *d, struct zwlr_layer_surface_v1 *s, uint32_t serial, uint32_t w, uint32_t h) { cfg_serial = serial; configured = 1; zwlr_layer_surface_v1_ack_configure(s, serial); }
static void ls_closed(void *d, struct zwlr_layer_surface_v1 *s) { exit(1); }
static const struct zwlr_layer_surface_v1_listener ls_l = {ls_configure, ls_closed};

static void fb_sync(void *d, struct wp_presentation_feedback *f, struct wl_output *o) {}
static void fb_presented(void *d, struct wp_presentation_feedback *f, uint32_t hi, uint32_t lo, uint32_t nsec, uint32_t refresh, uint32_t seqhi, uint32_t seqlo, uint32_t flags) {
    long long t = (((long long)hi << 32 | lo) * 1000000000LL) + nsec;
    fprintf(stderr, "%-24s %8.2f ms\n", "PRESENTED (compositor)", (t - t0) / 1e6);
    presented = 1;
}
static void fb_discarded(void *d, struct wp_presentation_feedback *f) { fprintf(stderr, "feedback discarded\n"); presented = 1; }
static const struct wp_presentation_feedback_listener fb_l = {fb_sync, fb_presented, fb_discarded};

int main(int argc, char **argv) {
    const char *mode = argc > 1 ? argv[1] : "shm";
    const char *e = getenv("T0_NS");
    t0 = e ? atoll(e) : now_ns();
    mark("main()");
    struct wl_display *dpy = wl_display_connect(NULL);
    if (!dpy) { fprintf(stderr, "no display\n"); return 2; }
    struct wl_registry *reg = wl_display_get_registry(dpy);
    wl_registry_add_listener(reg, &reg_l, NULL);
    wl_display_roundtrip(dpy);
    mark("globals");
    struct wl_surface *surf = wl_compositor_create_surface(compositor);
    struct zwlr_layer_surface_v1 *ls = zwlr_layer_shell_v1_get_layer_surface(layer_shell, surf, NULL, ZWLR_LAYER_SHELL_V1_LAYER_OVERLAY, "firstframe");
    zwlr_layer_surface_v1_add_listener(ls, &ls_l, NULL);
    zwlr_layer_surface_v1_set_size(ls, W, H);
    wl_surface_commit(surf);
    while (!configured) wl_display_dispatch(dpy);
    mark("configured");

    if (presentation) {
        struct wp_presentation_feedback *fb = wp_presentation_feedback(presentation, surf);
        wp_presentation_feedback_add_listener(fb, &fb_l, NULL);
    }

    if (!strcmp(mode, "shm")) {
        int fd = memfd_create("ff", MFD_CLOEXEC);
        ftruncate(fd, W * H * 4);
        uint32_t *px = mmap(NULL, W * H * 4, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
        for (int i = 0; i < W * H; i++) px[i] = 0xf0140d0b;
        struct wl_shm_pool *pool = wl_shm_create_pool(shm, fd, W * H * 4);
        struct wl_buffer *buf = wl_shm_pool_create_buffer(pool, 0, W, H, W * 4, WL_SHM_FORMAT_ARGB8888);
        wl_surface_attach(surf, buf, 0, 0);
        wl_surface_damage_buffer(surf, 0, 0, W, H);
        wl_surface_commit(surf);
        mark("committed");
    }
#ifdef WITH_EGL
    else if (!strcmp(mode, "egl")) {
        PFNEGLGETPLATFORMDISPLAYEXTPROC getdpy = (void *)eglGetProcAddress("eglGetPlatformDisplayEXT");
        EGLDisplay ed = getdpy(EGL_PLATFORM_WAYLAND_KHR, dpy, NULL);
        eglInitialize(ed, NULL, NULL);
        mark("egl initialized");
        eglBindAPI(EGL_OPENGL_ES_API);
        EGLint ca[] = {EGL_SURFACE_TYPE, EGL_WINDOW_BIT, EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT, EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_ALPHA_SIZE, 8, EGL_NONE};
        EGLConfig cfg; EGLint n;
        eglChooseConfig(ed, ca, &cfg, 1, &n);
        EGLint cxa[] = {EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE};
        EGLContext ctx = eglCreateContext(ed, cfg, EGL_NO_CONTEXT, cxa);
        struct wl_egl_window *ew = wl_egl_window_create(surf, W, H);
        EGLSurface es = eglCreateWindowSurface(ed, cfg, (EGLNativeWindowType)ew, NULL);
        eglMakeCurrent(ed, es, es, ctx);
        eglSwapInterval(ed, 0);
        mark("egl context current");
        glClearColor(0.08f, 0.05f, 0.04f, 0.94f);
        glClear(GL_COLOR_BUFFER_BIT);
        eglSwapBuffers(ed, es);
        mark("committed (swap)");
    }
#endif
#ifdef WITH_VK
    else if (!strcmp(mode, "vk")) {
        const char *iext[] = {VK_KHR_SURFACE_EXTENSION_NAME, VK_KHR_WAYLAND_SURFACE_EXTENSION_NAME};
        VkApplicationInfo ai = {.sType = VK_STRUCTURE_TYPE_APPLICATION_INFO, .apiVersion = VK_API_VERSION_1_1};
        VkInstanceCreateInfo ici = {.sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO, .pApplicationInfo = &ai, .enabledExtensionCount = 2, .ppEnabledExtensionNames = iext};
        VkInstance inst; if (vkCreateInstance(&ici, NULL, &inst)) { fprintf(stderr, "vkCreateInstance failed\n"); return 2; }
        mark("vk instance");
        uint32_t npd = 1; VkPhysicalDevice pd; vkEnumeratePhysicalDevices(inst, &npd, &pd);
        VkWaylandSurfaceCreateInfoKHR wsci = {.sType = VK_STRUCTURE_TYPE_WAYLAND_SURFACE_CREATE_INFO_KHR, .display = dpy, .surface = surf};
        VkSurfaceKHR vs; vkCreateWaylandSurfaceKHR(inst, &wsci, NULL, &vs);
        float prio = 1; VkDeviceQueueCreateInfo qci = {.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO, .queueFamilyIndex = 0, .queueCount = 1, .pQueuePriorities = &prio};
        const char *dext[] = {VK_KHR_SWAPCHAIN_EXTENSION_NAME};
        VkDeviceCreateInfo dci = {.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO, .queueCreateInfoCount = 1, .pQueueCreateInfos = &qci, .enabledExtensionCount = 1, .ppEnabledExtensionNames = dext};
        VkDevice dev; vkCreateDevice(pd, &dci, NULL, &dev);
        VkQueue q; vkGetDeviceQueue(dev, 0, 0, &q);
        mark("vk device");
        VkSwapchainCreateInfoKHR sci = {.sType = VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR, .surface = vs, .minImageCount = 2, .imageFormat = VK_FORMAT_B8G8R8A8_UNORM, .imageColorSpace = VK_COLOR_SPACE_SRGB_NONLINEAR_KHR, .imageExtent = {W, H}, .imageArrayLayers = 1, .imageUsage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT, .preTransform = VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR, .compositeAlpha = VK_COMPOSITE_ALPHA_PRE_MULTIPLIED_BIT_KHR, .presentMode = VK_PRESENT_MODE_MAILBOX_KHR, .clipped = VK_TRUE};
        VkSwapchainKHR sc; VkResult r = vkCreateSwapchainKHR(dev, &sci, NULL, &sc);
        if (r) { sci.presentMode = VK_PRESENT_MODE_FIFO_KHR; sci.compositeAlpha = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR; r = vkCreateSwapchainKHR(dev, &sci, NULL, &sc); }
        if (r) { fprintf(stderr, "swapchain failed %d\n", r); return 2; }
        uint32_t ni = 0; vkGetSwapchainImagesKHR(dev, sc, &ni, NULL); VkImage imgs[8]; vkGetSwapchainImagesKHR(dev, sc, &ni, imgs);
        mark("vk swapchain");
        VkCommandPoolCreateInfo cpi = {.sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO, .queueFamilyIndex = 0};
        VkCommandPool cp; vkCreateCommandPool(dev, &cpi, NULL, &cp);
        VkCommandBufferAllocateInfo cbi = {.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO, .commandPool = cp, .level = VK_COMMAND_BUFFER_LEVEL_PRIMARY, .commandBufferCount = 1};
        VkCommandBuffer cb; vkAllocateCommandBuffers(dev, &cbi, &cb);
        VkSemaphoreCreateInfo semi = {.sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
        VkSemaphore acq, done; vkCreateSemaphore(dev, &semi, NULL, &acq); vkCreateSemaphore(dev, &semi, NULL, &done);
        uint32_t idx; vkAcquireNextImageKHR(dev, sc, UINT64_MAX, acq, VK_NULL_HANDLE, &idx);
        VkCommandBufferBeginInfo bi = {.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};
        vkBeginCommandBuffer(cb, &bi);
        VkImageSubresourceRange rng = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1};
        VkImageMemoryBarrier b1 = {.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER, .dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT, .oldLayout = VK_IMAGE_LAYOUT_UNDEFINED, .newLayout = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, .image = imgs[idx], .subresourceRange = rng};
        vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, 0, 0, NULL, 0, NULL, 1, &b1);
        VkClearColorValue cc = {.float32 = {0.04f, 0.05f, 0.08f, 0.94f}};
        vkCmdClearColorImage(cb, imgs[idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &cc, 1, &rng);
        VkImageMemoryBarrier b2 = b1; b2.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT; b2.dstAccessMask = 0; b2.oldLayout = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL; b2.newLayout = VK_IMAGE_LAYOUT_PRESENT_SRC_KHR;
        vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT, 0, 0, NULL, 0, NULL, 1, &b2);
        vkEndCommandBuffer(cb);
        VkPipelineStageFlags ws = VK_PIPELINE_STAGE_TRANSFER_BIT;
        VkSubmitInfo si = {.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO, .waitSemaphoreCount = 1, .pWaitSemaphores = &acq, .pWaitDstStageMask = &ws, .commandBufferCount = 1, .pCommandBuffers = &cb, .signalSemaphoreCount = 1, .pSignalSemaphores = &done};
        vkQueueSubmit(q, 1, &si, VK_NULL_HANDLE);
        VkPresentInfoKHR pi = {.sType = VK_STRUCTURE_TYPE_PRESENT_INFO_KHR, .waitSemaphoreCount = 1, .pWaitSemaphores = &done, .swapchainCount = 1, .pSwapchains = &sc, .pImageIndices = &idx};
        vkQueuePresentKHR(q, &pi);
        mark("committed (present)");
    }
#endif
    else { fprintf(stderr, "mode %s not built\n", mode); return 2; }
    long long deadline = now_ns() + 2000000000LL;
    while (!presented && now_ns() < deadline) wl_display_dispatch(dpy);
    return 0;
}
