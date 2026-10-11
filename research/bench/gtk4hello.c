// Minimal GTK4 window with a few labels, for cold-start timing. Research tooling.
#include <gtk/gtk.h>
static void activate(GtkApplication *app, gpointer d) {
    GtkWidget *w = gtk_application_window_new(app);
    gtk_window_set_default_size(GTK_WINDOW(w), 640, 270);
    GtkWidget *box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
    const char *rows[] = {"theme  porpl", "mouse-autohide-toggle  off", "do-not-disturb  on", "keyboard-layout  us", "power-profile  balanced", "night-light  off"};
    for (int i = 0; i < 6; i++) gtk_box_append(GTK_BOX(box), gtk_label_new(rows[i]));
    gtk_window_set_child(GTK_WINDOW(w), box);
    gtk_window_present(GTK_WINDOW(w));
}
int main(int argc, char **argv) {
    GtkApplication *app = gtk_application_new("org.logis.bench", G_APPLICATION_NON_UNIQUE);
    g_signal_connect(app, "activate", G_CALLBACK(activate), NULL);
    return g_application_run(G_APPLICATION(app), argc, argv);
}
