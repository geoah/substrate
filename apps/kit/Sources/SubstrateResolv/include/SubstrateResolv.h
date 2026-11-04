#ifndef SUBSTRATE_RESOLV_H
#define SUBSTRATE_RESOLV_H

/// How the plain resolver (libresolv) answered one TXT query. The system
/// resolver behind DNSServiceQueryRecord reports a SERVFAIL with the same
/// NoSuchRecord as a name without the record, and these are what tell the
/// two apart; resolv.h does not import into Swift.
typedef enum {
    /// The name has TXT records.
    SubstrateTXTAnswered = 0,
    /// NXDOMAIN: the name does not exist.
    SubstrateTXTNoSuchName = 1,
    /// NOERROR with no TXT record.
    SubstrateTXTNoData = 2,
    /// SERVFAIL or no answer in time.
    SubstrateTXTTryAgain = 3,
    /// REFUSED, FORMERR, NOTIMP or a resolver that could not start.
    SubstrateTXTNoRecovery = 4,
} SubstrateTXTOutcome;

/// Asks the system's DNS servers for `name`'s TXT records and says how they
/// answered. Blocks for up to `timeoutSeconds` per try, two tries.
SubstrateTXTOutcome substrate_txt_outcome(const char *name, int timeoutSeconds);

#endif
