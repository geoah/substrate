#include "SubstrateResolv.h"

// The resolver this reads behind DNS-SD is Apple's; elsewhere the target
// is empty and nothing calls it.
#ifdef __APPLE__

#include <netdb.h>
#include <resolv.h>
#include <string.h>

SubstrateTXTOutcome substrate_txt_outcome(const char *name, int timeoutSeconds) {
    struct __res_state state;
    memset(&state, 0, sizeof state);
    if (res_ninit(&state) != 0) {
        return SubstrateTXTNoRecovery;
    }
    state.retrans = timeoutSeconds > 0 ? timeoutSeconds : 1;
    state.retry = 2;
    unsigned char answer[NS_PACKETSZ * 16];
    int length = res_nquery(&state, name, ns_c_in, ns_t_txt, answer, sizeof answer);
    int error = state.res_h_errno;
    res_ndestroy(&state);
    if (length >= 0) {
        return SubstrateTXTAnswered;
    }
    switch (error) {
    case HOST_NOT_FOUND:
        return SubstrateTXTNoSuchName;
    case NO_DATA:
        return SubstrateTXTNoData;
    case TRY_AGAIN:
        return SubstrateTXTTryAgain;
    default:
        return SubstrateTXTNoRecovery;
    }
}

#endif
