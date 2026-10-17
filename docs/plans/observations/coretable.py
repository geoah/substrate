from fractions import Fraction as F
import math
# exact definitions
IN=F(254,10000)        # m
FT=12*IN; YD=3*FT; MI=5280*FT; NMI=F(1852)
LB=F(45359237,10**8)   # kg
OZ=LB/16; ST=14*LB; GR=LB/7000
G0=F(980665,100000)    # m/s2
LBF=LB*G0
GAL_US=231*IN**3       # m3
GAL_BR=F(454609,10**8) # m3
FOZ_US=GAL_US/128; FOZ_BR=GAL_BR/160
TBS_US=FOZ_US/2; TSP_US=TBS_US/3; CUP_US=16*TBS_US
CAL=F(4184,1000)       # J (thermochemical, UCUM cal)
CAL_IT=F(41868,10000)
BTU_IT=F(105505585262,10**8)  # J (UCUM: 1.05505585262 kJ)
MMHG=F(133322,1000)    # Pa (UCUM m[Hg] = 133.3220 kPa)
CMH2O=G0*10            # Pa per cm water: m[H2O]=9.80665 kPa
PSI=LBF/IN**2
HP=550*FT*LBF          # W
EV=F(1602176634,10**28)# J
CI=F(37*10**9)         # Bq
H=3600; D=86400; MIN=60
rows=[]
def r(code,kind,factor,base,offset=None,note=''):
    rows.append((code,kind,factor,base,offset,note))
# mass (base kg)
for c,f in [('kg',1),('g',F(1,1000)),('mg',F(1,10**6)),('ug',F(1,10**9)),('ng',F(1,10**12)),('t',1000),
            ('[lb_av]',LB),('[oz_av]',OZ),('[stone_av]',ST)]:
    r(c,'mass',F(f),'kg')
# length (base m)
for c,f in [('m',1),('km',1000),('cm',F(1,100)),('mm',F(1,1000)),('nm',F(1,10**9)),
            ('[in_i]',IN),('[ft_i]',FT),('[yd_i]',YD),('[mi_i]',MI),('[nmi_i]',NMI)]:
    r(c,'length',F(f),'m')
# area
for c,f in [('m2',1),('cm2',F(1,10**4)),('har',10**4),('[sft_i]',FT**2),('[acr_us]',None)]:
    if f is None: continue
    r(c,'area',F(f),'m2')
# volume (base m3; L shown too)
for c,f in [('m3',1),('L',F(1,1000)),('dL',F(1,10**4)),('mL',F(1,10**6)),
            ('[gal_us]',GAL_US),('[qt_us]',GAL_US/4),('[pt_us]',GAL_US/8),('[cup_us]',CUP_US),('[foz_us]',FOZ_US),
            ('[tbs_us]',TBS_US),('[tsp_us]',TSP_US),('[gal_br]',GAL_BR),('[foz_br]',FOZ_BR),
            ('[cup_m]',F(240,10**6)),('[tbs_m]',F(15,10**6)),('[tsp_m]',F(5,10**6)),('[cft_i]',FT**3)]:
    r(c,'volume',F(f),'m3')
# time duration (base s)
for c,f in [('s',1),('ms',F(1,1000)),('us',F(1,10**6)),('min',60),('h',H),('d',D),('wk',7*D)]:
    r(c,'duration',F(f),'s')
r('mo','duration',F(D)*F(36525,100)/12,'s',note='UCUM mean Julian month 30.4375 d; not a calendar month')
r('a','duration',F(D)*F(36525,100),'s',note='UCUM Julian year 365.25 d; not a calendar year')
# temperature absolute (base K)
r('K','temperature',F(1),'K')
r('Cel','temperature',F(1),'K',offset=F(27315,100),note='affine')
r('[degF]','temperature',F(5,9),'K',offset=F(45967,100)*F(5,9),note='affine; K = (F + 459.67) x 5/9')
# temperature difference (same codes, no offset)
r('K','temperature-difference',F(1),'K')
r('Cel','temperature-difference',F(1),'K',note='offset suppressed')
r('[degF]','temperature-difference',F(5,9),'K',note='offset suppressed')
# energy (base J)
for c,f,n in [('J',1,''),('kJ',1000,''),('MJ',10**6,''),('cal',CAL,'UCUM cal = cal_th'),('kcal',1000*CAL,''),('[Cal]',1000*CAL,'nutrition Calorie = kcal_th'),
            ('W.h',3600,''),('kW.h',3600000,''),('eV',EV,'exact since 2019 SI'),('[Btu_IT]',BTU_IT,'')]:
    r(c,'energy',F(f),'J',note=n)
# power (base W)
for c,f,n in [('W',1,''),('mW',F(1,1000),''),('kW',1000,''),('MW',10**6,''),('[HP]',HP,'mechanical horsepower'),
              ('kcal/d',1000*CAL/D,'basal metabolic rate (Health Connect)'),('kcal/h',1000*CAL/H,''),('[Btu_IT]/h',BTU_IT/H,'HVAC'),('V.A',1,'apparent power: distinct kind, same factor')]:
    r(c,'power',F(f),'W',note=n)
# pressure (base Pa)
for c,f,n in [('Pa',1,''),('hPa',100,''),('kPa',1000,''),('bar',10**5,''),('mbar',100,''),('atm',101325,''),
              ('mm[Hg]',MMHG,'UCUM conventional 133.322 Pa'),('cm[H2O]',CMH2O,''),('[psi]',PSI,''),('[in_i\'Hg]',MMHG*1000*IN,'')]:
    r(c,'pressure',F(f),'Pa',note=n)
# speed (base m/s)
for c,f in [('m/s',1),('km/h',F(1000,H)),('[mi_i]/h',MI/H),('[kn_i]',NMI/H),('[ft_i]/s',FT)]:
    r(c,'speed',F(f),'m/s')
# pace (inverse speed, base s/m)
for c,f in [('s/m',1),('min/km',F(60,1000)),('s/km',F(1,1000)),('min/[mi_i]',60/MI),('s/(100.m)',F(1,100)),('s/(100.[yd_i])',1/(100*YD)),('s/(500.m)',F(1,500))]:
    r(c,'pace',F(f),'s/m')
# acceleration
r('m/s2','acceleration',F(1),'m/s2'); r('[g]','acceleration',G0,'m/s2')
# frequency / rate (base /s)
for c,f,k in [('Hz',1,'frequency'),('kHz',1000,'frequency'),
              ('/s',1,'rate'),('/min',F(1,60),'rate'),('/h',F(1,H),'rate'),('/d',F(1,D),'rate'),('/wk',F(1,7*D),'rate'),
              ]:
    r(c,k,F(f),'/s')
# count
r('{count}','count',F(1),'1',note='any {annotation} alone is a count; the annotation is stored, ignored in conversion')
r('{count}/min','rate',F(1,60),'/s',note='any {annotation}/min, e.g. {beats}/min, {steps}/min')
# fraction (base 1)
for c,f in [('1',1),('%',F(1,100)),('[ppth]',F(1,1000)),('[ppm]',F(1,10**6)),('[ppb]',F(1,10**9))]:
    r(c,'fraction',F(f),'1')
# amount of substance
for c,f in [('mol',1),('mmol',F(1,1000)),('umol',F(1,10**6))]:
    r(c,'amount',F(f),'mol')
# amount concentration (base mol/m3)
for c,f in [('mol/L',1000),('mmol/L',1),('umol/L',F(1,1000)),('nmol/L',F(1,10**6)),('pmol/L',F(1,10**9))]:
    r(c,'amount-concentration',F(f),'mol/m3')
r('mmol/mol','fraction',F(1,1000),'1',note='HbA1c IFCC; a fraction, not convertible to NGSP %')
# mass concentration (base kg/m3 = g/L)
for c,f in [('g/L',1),('g/dL',10),('mg/dL',F(1,100)),('mg/L',F(1,1000)),('ug/L',F(1,10**6)),('ng/mL',F(1,10**6)),('pg/mL',F(1,10**9)),
            ('ug/dL',F(1,10**5)),('g/m3',F(1,1000)),('mg/m3',F(1,10**6)),('ug/m3',F(1,10**9)),('kg/m3',1)]:
    r(c,'mass-concentration',F(f),'kg/m3')
# catalytic activity
r('U/L','catalytic-concentration',F(1,60*10**6)/F(1,1000),'mol/(s.m3)',note='1 U = 1 umol/min')
r('kat','catalytic-activity',F(1),'mol/s'); r('U','catalytic-activity',F(1,60*10**6),'mol/s')
# arbitrary
for c in ['[IU]','[IU]/L','m[IU]/L','[IU]/mL','[arb\'U]']:
    r(c,'arbitrary',None,'(per assay)',note='commensurable only with itself under one assay')
# light
r('lx','illuminance',F(1),'lx'); r('lm','luminous-flux',F(1),'lm'); r('cd','luminous-intensity',F(1),'cd'); r('cd/m2','luminance',F(1),'cd/m2')
r('W/m2','irradiance',F(1),'W/m2')
# levels (log)
r('dB','level',None,'B',note='log10, ratio unspecified; never converted, never summed')
r('dB[SPL]','sound-pressure-level',None,'20 uPa',note='special: 20 lg(p/20 uPa) dB')
r('dB[W]','power-level',None,'1 W',note='special: 10 lg(P/1 W) dB')
r('[pH]','acidity',None,'mol/L',note='special: -lg(a(H+)/(mol/L))')
# electrical
for c,f,k,b in [('V',1,'electric-potential','V'),('mV',F(1,1000),'electric-potential','V'),
                ('A',1,'electric-current','A'),('mA',F(1,1000),'electric-current','A'),
                ('Ohm',1,'electric-resistance','Ohm'),('kOhm',1000,'electric-resistance','Ohm'),
                ('C',1,'electric-charge','C'),('A.h',3600,'electric-charge','C'),('mA.h',F(3600,1000),'electric-charge','C'),
                ('uS/cm',F(1,10**4),'electric-conductivity','S/m')]:
    r(c,k,F(f),b)
# information
for c,f in [('bit',1),('By',8),('kBy',8000),('MBy',8*10**6),('GBy',8*10**9),('KiBy',8*1024),('MiBy',8*1024**2),('GiBy',8*1024**3)]:
    r(c,'information',F(f),'bit')
for c,f in [('bit/s',1),('kbit/s',1000),('Mbit/s',10**6),('By/s',8)]:
    r(c,'data-rate',F(f),'bit/s')
# angle (base deg, rad irrational)
r('deg','angle',F(1),'deg'); r("'",'angle',F(1,60),'deg'); r("''",'angle',F(1,3600),'deg')
r('rad','angle',None,'deg',note='180/pi deg: irrational')
# flows
for c,f in [('L/min',F(1,60000)),('L/h',F(1,3600000)),('mL/min',F(1,6*10**7)),('m3/h',F(1,3600)),('[gal_us]/min',GAL_US/60)]:
    r(c,'volume-flow',F(f),'m3/s')
r('g/s','mass-flow',F(1,1000),'kg/s')
# mechanics
r('N','force',F(1),'N'); r('[lbf_av]','force',LBF,'N')
r('N.m','torque',F(1),'N.m'); r('[lbf_av].[ft_i]','torque',LBF*FT,'N.m')
# radiation
r('Bq/m3','activity-concentration',F(1),'Bq/m3'); r('pCi/L','activity-concentration',F(37,1000)*1000,'Bq/m3',note='1 pCi/L = 37 Bq/m3')
r('uSv/h','dose-equivalent-rate',F(1,10**6*3600),'Sv/s')
# physiology / derived
r('kg/m2','body-mass-index',F(1),'kg/m2',note='same dimension as areal density, different kind')
r('mL/(kg.min)','oxygen-uptake-rate',F(1,6*10**7),'m3/(kg.s)')
r('[MET]','oxygen-uptake-rate',F(35,10)*F(1,6*10**7),'m3/(kg.s)',note='1 MET = 3.5 mL/min/kg')
r('W/kg','specific-power',F(1),'W/kg')
r('kcal/kg','specific-energy',1000*CAL,'J/kg')
# vehicle efficiency
r('kW.h/(100.km)','energy-per-distance',F(3600000,100000),'J/m',note='')
r('W.h/km','energy-per-distance',F(3600,1000),'J/m')
r('L/(100.km)','fuel-consumption',F(1,1000)/(100*1000),'m3/m',note='reciprocal of mpg; not a factor')
r('[mi_i]/[gal_us]','fuel-economy',MI/GAL_US,'m/m3',note='reciprocal of L/100km')
r('km/L','fuel-economy',F(1000)/F(1,1000),'m/m3')
r('mm/h','precipitation-rate',F(1,1000*3600),'m/s')
r('[in_i]/h','precipitation-rate',IN/3600,'m/s')

def finite(fr):
    d=fr.denominator
    for p in (2,5):
        while d%p==0: d//=p
    return d==1
def fmt(fr):
    if fr is None: return 'n/a'
    if fr.denominator==1: return str(fr.numerator)
    if finite(fr):
        # exact decimal
        s=('%.40f'%float(fr))
        from decimal import Decimal,getcontext
        getcontext().prec=60
        return format((Decimal(fr.numerator)/Decimal(fr.denominator)).normalize(),'f')
    return f'{fr.numerator}/{fr.denominator}'
import sys
codes=set(); out=[]
for code,kind,f,base,off,note in rows:
    codes.add(code)
    ex='exact' if f is None and False else ('n/a' if f is None else ('finite decimal' if finite(f) else 'rational, no finite decimal'))
    offs='' if off is None else fmt(off)
    out.append(f'| `{code}` | {kind} | {fmt(f)} {base if f is not None else ""} | {offs} | {ex} | {note} |')
print('\n'.join(out))
print('ROWS',len(rows),'DISTINCT CODES',len(codes), 'KINDS',len(set(r[1] for r in rows)), file=sys.stderr)
from collections import Counter
print(Counter(r[1] for r in rows), file=sys.stderr)
