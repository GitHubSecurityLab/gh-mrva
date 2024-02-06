/**
 * @name pickfun
 * @description pick function from FlatBuffers
 * @kind problem
 * @id cpp-flatbuffer-func
 * @problem.severity warning
 */

import cpp

from Function f
where f.getName() = "MakeBinaryRegion"
select f, "definition of MakeBinaryRegion"
