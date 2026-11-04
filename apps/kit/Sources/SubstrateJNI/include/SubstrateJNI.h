#ifndef SUBSTRATE_JNI_H
#define SUBSTRATE_JNI_H

// What SubstrateBridge reads from the NDK: JNI, the Looper and the log. Empty
// off Android, so the target builds everywhere the kit does.
#ifdef __ANDROID__
#include <android/log.h>
#include <android/looper.h>
#include <dlfcn.h>
#include <jni.h>
#include <pthread.h>

// libdispatch's main queue, as CoreFoundation's run loop drives it on Linux:
// exported by libdispatch.so but declared in no public header.
int _dispatch_get_main_queue_handle_4CF(void);
void _dispatch_main_queue_callback_4CF(void *_Nullable msg);
#endif

#endif
